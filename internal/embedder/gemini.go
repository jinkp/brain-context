package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const geminiEndpoint = "https://generativelanguage.googleapis.com/v1beta/models/text-embedding-004:batchEmbedContents"

type geminiEmbedder struct {
	model      string
	apiKey     string
	dimensions int
	client     *http.Client
}

type geminiBatchRequest struct {
	Requests []geminiEmbedRequest `json:"requests"`
}

type geminiEmbedRequest struct {
	Model                string        `json:"model"`
	Content              geminiContent `json:"content"`
	OutputDimensionality int           `json:"outputDimensionality,omitempty"`
	TaskType             string        `json:"taskType,omitempty"`
}

type geminiContent struct {
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text string `json:"text"`
}

type geminiBatchResponse struct {
	Embeddings []struct {
		Values []float32 `json:"values"`
	} `json:"embeddings"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

func newGeminiEmbedder(model, apiKey string, dimensions int) Embedder {
	return &geminiEmbedder{
		model:      strings.TrimPrefix(model, ProviderGemini+"/"),
		apiKey:     strings.TrimSpace(apiKey),
		dimensions: dimensions,
		client:     &http.Client{Timeout: 30 * time.Second},
	}
}

func (e *geminiEmbedder) Dimensions() int {
	return e.dimensions
}

func (e *geminiEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if strings.TrimSpace(e.apiKey) == "" {
		return nil, fmt.Errorf("gemini api key is required")
	}
	requests := make([]geminiEmbedRequest, 0, len(texts))
	for _, text := range texts {
		requests = append(requests, geminiEmbedRequest{
			Model:                "models/" + e.model,
			OutputDimensionality: e.dimensions,
			TaskType:             "RETRIEVAL_DOCUMENT",
			Content:              geminiContent{Parts: []geminiPart{{Text: text}}},
		})
	}
	payload, err := json.Marshal(geminiBatchRequest{Requests: requests})
	if err != nil {
		return nil, fmt.Errorf("marshal gemini request: %w", err)
	}
	endpoint := geminiEndpoint + "?key=" + url.QueryEscape(e.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create gemini request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call gemini embeddings api: %w", err)
	}
	defer resp.Body.Close()

	var body geminiBatchResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decode gemini response: %w", err)
	}
	if resp.StatusCode >= 400 {
		message := resp.Status
		if body.Error != nil && body.Error.Message != "" {
			message = body.Error.Message
		}
		return nil, fmt.Errorf("gemini embeddings api returned %s", message)
	}
	if len(body.Embeddings) != len(texts) {
		return nil, fmt.Errorf("gemini embeddings api returned %d embeddings for %d texts", len(body.Embeddings), len(texts))
	}
	result := make([][]float32, 0, len(body.Embeddings))
	for _, embedding := range body.Embeddings {
		result = append(result, embedding.Values)
	}
	return result, nil
}
