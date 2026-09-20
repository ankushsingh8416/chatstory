package vectorstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/shridarpatil/whatomate/internal/models"
	"github.com/shridarpatil/whatomate/internal/netutil"
)

// validIdentifier matches a safe SQL identifier. Table/collection names come
// from org input and are interpolated into DDL/DML (Postgres doesn't support
// parameterizing identifiers), so every use is validated against this first.
var validIdentifier = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]{0,62}$`)

func quoteIdentifier(name string) (string, error) {
	if !validIdentifier.MatchString(name) {
		return "", fmt.Errorf("invalid collection name %q: must start with a letter or underscore and contain only letters, digits, and underscores (max 63 chars)", name)
	}
	return `"` + name + `"`, nil
}

// vectorLiteral formats a []float32 as a pgvector text literal, e.g.
// "[0.1,0.2,0.3]". pgvector accepts this cast to ::vector — no extra Go
// driver/extension package needed.
func vectorLiteral(v []float32) string {
	parts := make([]string, len(v))
	for i, f := range v {
		parts[i] = strconv.FormatFloat(float64(f), 'f', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// pgVectorStore talks to an org's own Postgres/Supabase instance (with the
// pgvector extension) over the standard wire protocol. This is a completely
// separate connection from the app's own GORM database — different
// host/credentials entirely, opened fresh per use rather than pooled
// long-term, so an org editing or deleting their connection can never leave
// a stale pool holding old credentials.
type pgVectorStore struct {
	dsn string
}

func newPgVectorStore(conn models.DataConnection) (*pgVectorStore, error) {
	if conn.Host == "" || conn.Database == "" {
		return nil, fmt.Errorf("postgres connection is missing host or database")
	}
	sslMode := conn.SSLMode
	if sslMode == "" {
		sslMode = "require"
	}
	port := conn.Port
	if port == 0 {
		port = 5432
	}

	dsn := (&url.URL{
		Scheme:   "postgres",
		User:     url.UserPassword(conn.Username, conn.Password),
		Host:     fmt.Sprintf("%s:%d", conn.Host, port),
		Path:     "/" + conn.Database,
		RawQuery: url.Values{"sslmode": {sslMode}}.Encode(),
	}).String()

	return &pgVectorStore{dsn: dsn}, nil
}

// open builds a short-lived *sql.DB for a single operation. The dial path is
// SSRF-safe (netutil.SSRFSafeDialer) so an org can't point this connection at
// the app's own internal network (Redis, other services) by supplying a
// private-range host. Callers must Close the returned DB when done.
func (p *pgVectorStore) open() (*sql.DB, error) {
	cfg, err := pgx.ParseConfig(p.dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid postgres connection: %w", err)
	}
	cfg.DialFunc = netutil.SSRFSafeDialer()
	// Many orgs hand us their Supabase *pooled* connection string (port 6543,
	// PgBouncer in transaction mode) rather than the direct one - pgx's
	// default server-side prepared-statement caching then collides across
	// pooled connections ("prepared statement already exists", since
	// PgBouncer can hand different client sessions the same backend
	// connection without resetting its prepared-statement state). The simple
	// query protocol avoids server-side prepared statements entirely, which
	// works correctly against both direct and pooled connections.
	cfg.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	db := stdlib.OpenDB(*cfg)
	// Small, short-lived pool — this is one external tenant DB, not app traffic.
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(2 * time.Minute)
	db.SetConnMaxIdleTime(30 * time.Second)
	return db, nil
}

func (p *pgVectorStore) Ping(ctx context.Context) error {
	db, err := p.open()
	if err != nil {
		return err
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	return nil
}

// EnsureCollection creates the pgvector extension and target table if they
// don't already exist. The table is a fixed shape: id (uuid PK), content
// (text), embedding (vector(dims)), metadata (jsonb).
func (p *pgVectorStore) EnsureCollection(ctx context.Context, collection string, dims int) error {
	quoted, err := quoteIdentifier(collection)
	if err != nil {
		return err
	}
	if dims <= 0 {
		return fmt.Errorf("embedding dimensions must be positive")
	}

	db, err := p.open()
	if err != nil {
		return err
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	if _, err := db.ExecContext(ctx, `CREATE EXTENSION IF NOT EXISTS vector`); err != nil {
		return fmt.Errorf("failed to enable pgvector extension: %w", err)
	}

	createSQL := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s (id uuid PRIMARY KEY, content text, embedding vector(%d), metadata jsonb)`,
		quoted, dims,
	)
	if _, err := db.ExecContext(ctx, createSQL); err != nil {
		return fmt.Errorf("failed to create table: %w", err)
	}
	return nil
}

// Upsert writes chunk vectors as rows, keyed by VectorRef (id).
func (p *pgVectorStore) Upsert(ctx context.Context, collection string, items []UpsertItem) error {
	if len(items) == 0 {
		return nil
	}
	quoted, err := quoteIdentifier(collection)
	if err != nil {
		return err
	}

	db, err := p.open()
	if err != nil {
		return err
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	stmt := fmt.Sprintf(
		`INSERT INTO %s (id, content, embedding, metadata) VALUES ($1, $2, $3::vector, $4)
		 ON CONFLICT (id) DO UPDATE SET content = EXCLUDED.content, embedding = EXCLUDED.embedding, metadata = EXCLUDED.metadata`,
		quoted,
	)
	for _, item := range items {
		metaJSON, err := json.Marshal(item.Metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}
		if _, err := tx.ExecContext(ctx, stmt, item.ID, item.Content, vectorLiteral(item.Vector), metaJSON); err != nil {
			return fmt.Errorf("failed to upsert chunk %s: %w", item.ID, err)
		}
	}

	return tx.Commit()
}

// Delete removes rows by id.
func (p *pgVectorStore) Delete(ctx context.Context, collection string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	quoted, err := quoteIdentifier(collection)
	if err != nil {
		return err
	}

	db, err := p.open()
	if err != nil {
		return err
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}
	stmt := fmt.Sprintf(`DELETE FROM %s WHERE id IN (%s)`, quoted, strings.Join(placeholders, ","))
	if _, err := db.ExecContext(ctx, stmt, args...); err != nil {
		return fmt.Errorf("failed to delete chunks: %w", err)
	}
	return nil
}

// ColumnInfo describes one column of an introspected table.
type ColumnInfo struct {
	Name       string `json:"name"`
	DataType   string `json:"data_type"`
	IsVector   bool   `json:"is_vector"`
	VectorDims int    `json:"vector_dims,omitempty"` // 0 if not a vector column or dims couldn't be read
}

// TableInfo describes one introspected table and whether it looks usable as
// an existing knowledge-base collection (has at least one vector column).
type TableInfo struct {
	Name            string       `json:"name"`
	Columns         []ColumnInfo `json:"columns"`
	HasVectorColumn bool         `json:"has_vector_column"`
}

// ListPostgresTables introspects an org's connected Postgres/Supabase
// database (read-only) so the Knowledge Base UI can offer "point at my
// existing vector table" instead of requiring every org to hand-type exact
// table/column names for a schema only they know.
func ListPostgresTables(conn models.DataConnection, encryptionKey string) ([]TableInfo, error) {
	conn.DecryptSecrets(encryptionKey)
	store, err := newPgVectorStore(conn)
	if err != nil {
		return nil, err
	}

	db, err := store.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	rows, err := db.QueryContext(ctx, `
		SELECT table_name, column_name, data_type, udt_name
		FROM information_schema.columns
		WHERE table_schema = 'public'
		ORDER BY table_name, ordinal_position
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to list tables: %w", err)
	}
	defer rows.Close()

	order := []string{}
	byTable := map[string]*TableInfo{}
	type vectorCol struct{ table, column string }
	var vectorCols []vectorCol

	for rows.Next() {
		var table, column, dataType, udtName string
		if err := rows.Scan(&table, &column, &dataType, &udtName); err != nil {
			return nil, fmt.Errorf("failed to scan column info: %w", err)
		}
		info, ok := byTable[table]
		if !ok {
			info = &TableInfo{Name: table}
			byTable[table] = info
			order = append(order, table)
		}
		isVector := udtName == "vector"
		info.Columns = append(info.Columns, ColumnInfo{Name: column, DataType: dataType, IsVector: isVector})
		if isVector {
			info.HasVectorColumn = true
			vectorCols = append(vectorCols, vectorCol{table, column})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Best-effort: read the actual dimension of each vector column from a
	// real row (pgvector's vector_dims()) - more reliable than the column's
	// declared type modifier, which is 0/unset for a bare "vector" column.
	for _, vc := range vectorCols {
		quotedTable, err := quoteIdentifier(vc.table)
		if err != nil {
			continue
		}
		quotedCol, err := quoteIdentifier(vc.column)
		if err != nil {
			continue
		}
		var dims int
		q := fmt.Sprintf(`SELECT vector_dims(%s) FROM %s WHERE %s IS NOT NULL LIMIT 1`, quotedCol, quotedTable, quotedCol)
		if err := db.QueryRowContext(ctx, q).Scan(&dims); err == nil {
			info := byTable[vc.table]
			for i := range info.Columns {
				if info.Columns[i].Name == vc.column {
					info.Columns[i].VectorDims = dims
				}
			}
		}
	}

	result := make([]TableInfo, 0, len(order))
	for _, name := range order {
		result = append(result, *byTable[name])
	}
	return result, nil
}

// Search returns the topK rows nearest to vector by cosine distance
// (pgvector's <=> operator; smaller is more similar). Score is reported as
// 1 - distance so it reads the same way as Qdrant's cosine similarity.
// contentColumn/embeddingColumn default to "content"/"embedding" (this
// app's own ingestion pipeline shape) when empty, but can name any text +
// vector(N) column pair — e.g. an org's own pre-existing table.
func (p *pgVectorStore) Search(ctx context.Context, collection string, vector []float32, topK int, contentColumn, embeddingColumn string) ([]SearchResult, error) {
	if contentColumn == "" {
		contentColumn = "content"
	}
	if embeddingColumn == "" {
		embeddingColumn = "embedding"
	}
	quoted, err := quoteIdentifier(collection)
	if err != nil {
		return nil, err
	}
	quotedContent, err := quoteIdentifier(contentColumn)
	if err != nil {
		return nil, err
	}
	quotedEmbedding, err := quoteIdentifier(embeddingColumn)
	if err != nil {
		return nil, err
	}

	db, err := p.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	stmt := fmt.Sprintf(
		`SELECT %s, 1 - (%s <=> $1::vector) AS score FROM %s ORDER BY %s <=> $1::vector LIMIT $2`,
		quotedContent, quotedEmbedding, quoted, quotedEmbedding,
	)
	rows, err := db.QueryContext(ctx, stmt, vectorLiteral(vector), topK)
	if err != nil {
		return nil, fmt.Errorf("failed to search: %w", err)
	}
	defer rows.Close()

	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.Content, &r.Score); err != nil {
			return nil, fmt.Errorf("failed to scan search result: %w", err)
		}
		results = append(results, r)
	}
	return results, rows.Err()
}
