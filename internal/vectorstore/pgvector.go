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
