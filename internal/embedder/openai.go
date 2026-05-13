package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const openAIEndpoint = "https://api.openai.com/v1/embeddings"

type openAIEmbedder struct {
	model      string
	apiKey     string
	dimensions int
	client     *http.Client
}

type openAIRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions int      `json:"dimensions,omitempty"`
}

type openAIResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func newOpenAIEmbedder(model, apiKey string, dimensions int) Embedder {
	return &openAIEmbedder{
		model:      strings.TrimPrefix(model, ProviderOpenAI+"/"),
		apiKey:     strings.TrimSpace(apiKey),
		dimensions: dimensions,
		client:     &http.Client{Timeout: 120 * time.Second},
	}
}

func (e *openAIEmbedder) Dimensions() int {
	return e.dimensions
}

const openAIBatchSize = 25 // Conservative: 25 chunks × ~5K tokens = ~125K tokens per request (limit 300K)

func (e *openAIEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if strings.TrimSpace(e.apiKey) == "" {
		return nil, fmt.Errorf("openai api key is required")
	}

	result := make([][]float32, 0, len(texts))
	totalBatches := (len(texts) + openAIBatchSize - 1) / openAIBatchSize

	for i := 0; i < len(texts); i += openAIBatchSize {
		end := i + openAIBatchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[i:end]
		batchNum := i/openAIBatchSize + 1

		if totalBatches > 1 {
			fmt.Printf("   batch %d/%d (%d chunks)...\n", batchNum, totalBatches, len(batch))
		}

		embeddings, err := e.embedBatch(ctx, batch)
		if err != nil {
			// Return partial results so cache can save what we got
			return result, fmt.Errorf("batch %d/%d failed: %w (previous batches cached)", batchNum, totalBatches, err)
		}
		result = append(result, embeddings...)
	}

	return result, nil
}

const openAIMaxCharsPerInput = 20000 // ~5000 tokens — well under OpenAI's 8192 token limit

func (e *openAIEmbedder) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	// Truncate any input that exceeds the character limit
	truncated := make([]string, len(texts))
	maxChars := openAIMaxCharsPerInput
	for i, t := range texts {
		if len(t) > maxChars {
			truncated[i] = t[:maxChars]
		} else {
			truncated[i] = t
		}
	}
	texts = truncated

	payload, err := json.Marshal(openAIRequest{
		Model:      e.model,
		Input:      texts,
		Dimensions: e.dimensions,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal openai request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openAIEndpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create openai request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call openai embeddings api: %w", err)
	}
	defer resp.Body.Close()

	var body openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode openai response: %w", err)
	}
	if resp.StatusCode >= 400 {
		message := resp.Status
		if body.Error != nil && body.Error.Message != "" {
			message = body.Error.Message
		}
		return nil, fmt.Errorf("openai embeddings api returned %s", message)
	}
	result := make([][]float32, len(body.Data))
	for _, item := range body.Data {
		if item.Index < 0 || item.Index >= len(body.Data) {
			return nil, fmt.Errorf("openai embeddings api returned invalid index %d", item.Index)
		}
		result[item.Index] = item.Embedding
	}
	if len(result) != len(texts) {
		return nil, fmt.Errorf("openai embeddings api returned %d embeddings for %d texts", len(result), len(texts))
	}
	return result, nil
}
