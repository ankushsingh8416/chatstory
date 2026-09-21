package vectorstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/shridarpatil/whatomate/internal/models"
)

// qdrantStore talks to a Qdrant instance over its plain HTTP/JSON REST API.
// httpClient is expected to already be SSRF-safe (the app's shared
// a.HTTPClient, built with netutil.SSRFSafeDialer in main.go) — this type
// adds no dialing logic of its own, it just shapes requests.
type qdrantStore struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

func newQdrantStore(conn models.DataConnection, httpClient *http.Client) *qdrantStore {
	return &qdrantStore{
		baseURL:    strings.TrimRight(conn.QdrantURL, "/"),
		apiKey:     conn.QdrantAPIKey,
		httpClient: httpClient,
	}
}

func (q *qdrantStore) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, q.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	if q.apiKey != "" {
		req.Header.Set("api-key", q.apiKey)
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

// Ping lists collections — a lightweight call that exercises both
// connectivity and authentication in one round trip.
func (q *qdrantStore) Ping(ctx context.Context) error {
	if q.baseURL == "" {
		return fmt.Errorf("qdrant URL is not set")
	}

	req, err := q.newRequest(ctx, http.MethodGet, "/collections", nil)
	if err != nil {
		return err
	}

	resp, err := q.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reach Qdrant: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("qdrant rejected the API key (status %d)", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return fmt.Errorf("qdrant returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return nil
}

// doJSON sends a JSON request and returns the response body if the status is
// 2xx, otherwise an error including the response body for diagnostics.
func (q *qdrantStore) doJSON(ctx context.Context, method, path string, payload any) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}

	req, err := q.newRequest(ctx, method, path, body)
	if err != nil {
		return nil, err
	}

	resp, err := q.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to reach Qdrant: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("qdrant returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	return respBody, nil
}

// EnsureCollection creates the collection if it doesn't already exist.
func (q *qdrantStore) EnsureCollection(ctx context.Context, collection string, dims int) error {
	// Check first — Qdrant errors on PUT if a collection with a different
	// vector size already exists, so avoid re-creating one that's already fine.
	req, err := q.newRequest(ctx, http.MethodGet, "/collections/"+collection, nil)
	if err != nil {
		return err
	}
	resp, err := q.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to reach Qdrant: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil // already exists
	}

	_, err = q.doJSON(ctx, http.MethodPut, "/collections/"+collection, map[string]any{
		"vectors": map[string]any{
			"size":     dims,
			"distance": "Cosine",
		},
	})
	return err
}

// Upsert writes chunk vectors as Qdrant points, keyed by VectorRef (a UUID
// string — Qdrant accepts UUID or unsigned-integer point IDs).
func (q *qdrantStore) Upsert(ctx context.Context, collection string, items []UpsertItem) error {
	if len(items) == 0 {
		return nil
	}
	points := make([]map[string]any, len(items))
	for i, item := range items {
		payload := map[string]any{"content": item.Content}
		for k, v := range item.Metadata {
			payload[k] = v
		}
		points[i] = map[string]any{
			"id":      item.ID,
			"vector":  item.Vector,
			"payload": payload,
		}
	}
	_, err := q.doJSON(ctx, http.MethodPut, "/collections/"+collection+"/points?wait=true", map[string]any{
		"points": points,
	})
	return err
}

// Delete removes points by ID.
func (q *qdrantStore) Delete(ctx context.Context, collection string, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := q.doJSON(ctx, http.MethodPost, "/collections/"+collection+"/points/delete?wait=true", map[string]any{
		"points": ids,
	})
	return err
}

// Search returns the topK points nearest to vector, by cosine similarity
// (the distance metric the collection was created with in EnsureCollection).
// contentColumns' first entry selects which payload field holds the text
// (Qdrant has no "columns", and no way to concatenate payload fields
// server-side, so only the first name is used); embeddingColumn is unused
// (Qdrant vectors aren't a payload field).
func (q *qdrantStore) Search(ctx context.Context, collection string, vector []float32, topK int, contentColumns []string, _ string) ([]SearchResult, error) {
	contentColumn := "content"
	if len(contentColumns) > 0 && contentColumns[0] != "" {
		contentColumn = contentColumns[0]
	}
	respBody, err := q.doJSON(ctx, http.MethodPost, "/collections/"+collection+"/points/search", map[string]any{
		"vector":       vector,
		"limit":        topK,
		"with_payload": true,
	})
	if err != nil {
		return nil, err
	}

	var parsed struct {
		Result []struct {
			ID      any            `json:"id"`
			Score   float64        `json:"score"`
			Payload map[string]any `json:"payload"`
		} `json:"result"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("failed to parse search response: %w", err)
	}

	results := make([]SearchResult, 0, len(parsed.Result))
	for _, r := range parsed.Result {
		content, _ := r.Payload[contentColumn].(string)
		results = append(results, SearchResult{
			ID:      fmt.Sprint(r.ID),
			Content: content,
			Score:   r.Score,
		})
	}
	return results, nil
}
