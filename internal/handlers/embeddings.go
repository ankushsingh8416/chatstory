package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// openAIEmbeddingModel is the model used for knowledge-base document
// embeddings — independent of whichever provider (OpenAI or Anthropic)
// generates the chatbot's chat replies, since Anthropic has no first-party
// embeddings API. See the "openai_embeddings_key_encrypted" org setting.
const openAIEmbeddingModel = "text-embedding-3-small"

// openAIEmbeddingBatchSize caps how many texts go into one embeddings call —
// comfortably under OpenAI's per-request input-array limit.
const openAIEmbeddingBatchSize = 64

// generateOpenAIEmbeddings embeds a batch of texts in one API call, mirroring
// the HTTP-call shape of generateOpenAIResponse in chatbot_processor.go.
// Callers with more than openAIEmbeddingBatchSize texts should batch
// themselves (kept as a caller concern so partial-batch failures are
// attributable to a specific slice of chunks).
//
// dimensions, when > 0, is passed to OpenAI's `dimensions` parameter to
// truncate the output (text-embedding-3-* models support this natively -
// "Matryoshka" truncation, not a lossy resize). This matters for a
// knowledge base pointed at an org's own pre-existing table: its vectors
// may have been generated with a smaller dimension than this model's
// 1536-dim default, and cosine similarity is only meaningful between
// vectors of the same length. Pass 0 to use the model's default.
func (a *App) generateOpenAIEmbeddings(apiKey string, texts []string, dimensions int) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	payload := map[string]any{
		"model": openAIEmbeddingModel,
		"input": texts,
	}
	if dimensions > 0 {
		payload["dimensions"] = dimensions
	}
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}

	req, err := http.NewRequest("POST", "https://api.openai.com/v1/embeddings", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := a.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		var errResp struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(body, &errResp)
		if errResp.Error.Message != "" {
			return nil, fmt.Errorf("OpenAI API error: %s", errResp.Error.Message)
		}
		return nil, fmt.Errorf("OpenAI API returned status %d", resp.StatusCode)
	}

	var result struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	embeddings := make([][]float32, len(texts))
	for _, d := range result.Data {
		if d.Index < 0 || d.Index >= len(embeddings) {
			continue
		}
		embeddings[d.Index] = d.Embedding
	}
	for i, e := range embeddings {
		if e == nil {
			return nil, fmt.Errorf("OpenAI did not return an embedding for input %d", i)
		}
	}

	return embeddings, nil
}
