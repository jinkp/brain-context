package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultOllamaEndpoint = "http://localhost:11434/api/embeddings"

type ollamaEmbedder struct {
	model      string
	dimensions int
	endpoint   string
	client     *http.Client
}

type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaResponse struct {
	Embedding []float32 `json:"embedding"`
	Error     string    `json:"error,omitempty"`
}

func newOllamaEmbedder(model string, dimensions int) Embedder {
	endpoint := strings.TrimRight(strings.TrimSpace(os.Getenv("OLLAMA_BASE_URL")), "/")
	if endpoint == "" {
		endpoint = strings.TrimSuffix(defaultOllamaEndpoint, "/api/embeddings")
	}
	return &ollamaEmbedder{
		model:      strings.TrimPrefix(model, ProviderOllama+"/"),
		dimensions: dimensions,
		endpoint:   endpoint + "/api/embeddings",
		client:     &http.Client{Timeout: 120 * time.Second},
	}
}

func (e *ollamaEmbedder) Dimensions() int {
	return e.dimensions
}

func (e *ollamaEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	result := make([][]float32, 0, len(texts))
	for _, text := range texts {
		payload, err := json.Marshal(ollamaRequest{Model: e.model, Prompt: text})
		if err != nil {
			return nil, fmt.Errorf("marshal ollama request: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("create ollama request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := e.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("call ollama embeddings api: %w", err)
		}

		var body ollamaResponse
		decodeErr := json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if decodeErr != nil {
			return nil, fmt.Errorf("decode ollama response: %w", decodeErr)
		}
		if resp.StatusCode >= 400 {
			message := resp.Status
			if body.Error != "" {
				message = body.Error
			}
			return nil, fmt.Errorf("ollama embeddings api returned %s", message)
		}
		result = append(result, body.Embedding)
	}
	return result, nil
}
