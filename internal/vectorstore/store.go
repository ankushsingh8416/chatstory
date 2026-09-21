// Package vectorstore abstracts the external vector/DB backends an
// organization can bring their own credentials for (Qdrant, Postgres/Supabase
// with pgvector — more can be added later without touching callers). A Store
// is built fresh from a models.DataConnection per use; nothing is held or
// pooled long-term across requests, since orgs can edit or delete their
// connection at any time.
package vectorstore

import (
	"context"
	"fmt"
	"net/http"

	"github.com/shridarpatil/whatomate/internal/models"
)

// UpsertItem is one chunk being written to the vector store: its embedding
// plus the text and metadata needed to reconstruct/display it later.
type UpsertItem struct {
	ID       string // VectorRef — stable so re-ingestion can overwrite in place
	Vector   []float32
	Content  string
	Metadata map[string]any
}

// SearchResult is one match returned by Search, ordered by relevance.
type SearchResult struct {
	ID      string // VectorRef
	Content string
	Score   float64 // similarity score, backend-defined scale (cosine similarity: -1..1)
}

// Store is the contract every backend implements.
type Store interface {
	// Ping verifies the connection is reachable and the credentials work.
	Ping(ctx context.Context) error
	// EnsureCollection creates the collection/table if it doesn't already
	// exist, sized for the given embedding dimensionality.
	EnsureCollection(ctx context.Context, collection string, dims int) error
	// Upsert writes (or overwrites, by ID) chunk vectors into collection.
	Upsert(ctx context.Context, collection string, items []UpsertItem) error
	// Delete removes vectors by ID — used when a document is re-ingested or
	// removed, so stale vectors don't linger and pollute retrieval later.
	Delete(ctx context.Context, collection string, ids []string) error
	// Search returns the topK chunks most similar to vector, used for
	// chatbot RAG retrieval. contentColumns/embeddingColumn name the text and
	// vector columns to read (Postgres only — an org's pre-existing table
	// may not use this app's default "content"/"embedding" names). Passing
	// more than one contentColumns entry concatenates them (Postgres only —
	// e.g. a title column plus a body column read as one combined chunk of
	// text); pass nil/empty for both to use the default.
	Search(ctx context.Context, collection string, vector []float32, topK int, contentColumns []string, embeddingColumn string) ([]SearchResult, error)
}

// NewStore builds a Store for the given connection. conn is taken by value
// and its secrets decrypted into a local copy — the caller's original struct
// is never mutated, so a decrypted credential can't accidentally leak out
// through a shared pointer.
func NewStore(conn models.DataConnection, encryptionKey string, httpClient *http.Client) (Store, error) {
	conn.DecryptSecrets(encryptionKey)

	switch conn.Type {
	case models.DataConnectionTypeQdrant:
		return newQdrantStore(conn, httpClient), nil
	case models.DataConnectionTypePostgres:
		return newPgVectorStore(conn)
	default:
		return nil, fmt.Errorf("unsupported data connection type: %s", conn.Type)
	}
}
