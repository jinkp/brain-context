package uploader

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultBatchSize = 50
	defaultRetries   = 3
)

type ChunkPayload struct {
	ChunkHash  string    `json:"chunk_hash"`
	SymbolName string    `json:"symbol_name"`
	SymbolType string    `json:"symbol_type"`
	StartLine  int       `json:"start_line"`
	EndLine    int       `json:"end_line"`
	FilePath   string    `json:"file_path"`
	Language   string    `json:"language"`
	TokenCount int       `json:"token_count"`
	Embedding  []float32 `json:"embedding"`
}

type UploadResponse struct {
	Accepted int      `json:"accepted"`
	Skipped  int      `json:"skipped"`
	Errors   []string `json:"errors"`
}

type apiErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

type Client struct {
	BaseURL      string
	ProjectID    string
	ProjectToken string
	HTTPClient   *http.Client
	BatchSize    int
	MaxRetries   int
}

func (c Client) Upload(ctx context.Context, chunks []ChunkPayload) (UploadResponse, error) {
	if len(chunks) == 0 {
		return UploadResponse{}, nil
	}
	if strings.TrimSpace(c.BaseURL) == "" {
		return UploadResponse{}, fmt.Errorf("uploader base url is required")
	}
	if strings.TrimSpace(c.ProjectID) == "" {
		return UploadResponse{}, fmt.Errorf("uploader project id is required")
	}
	if strings.TrimSpace(c.ProjectToken) == "" {
		return UploadResponse{}, fmt.Errorf("uploader project token is required")
	}

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}

	batchSize := c.BatchSize
	if batchSize <= 0 || batchSize > defaultBatchSize {
		batchSize = defaultBatchSize
	}

	maxRetries := c.MaxRetries
	if maxRetries <= 0 {
		maxRetries = defaultRetries
	}

	var combined UploadResponse
	for start := 0; start < len(chunks); start += batchSize {
		end := start + batchSize
		if end > len(chunks) {
			end = len(chunks)
		}

		response, err := c.uploadBatch(ctx, httpClient, chunks[start:end], maxRetries)
		if err != nil {
			return UploadResponse{}, err
		}
		combined.Accepted += response.Accepted
		combined.Skipped += response.Skipped
		combined.Errors = append(combined.Errors, response.Errors...)
	}

	return combined, nil
}

func (c Client) uploadBatch(ctx context.Context, httpClient *http.Client, chunks []ChunkPayload, maxRetries int) (UploadResponse, error) {
	payload, err := json.Marshal(chunks)
	if err != nil {
		return UploadResponse{}, fmt.Errorf("marshal upload payload: %w", err)
	}

	url := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/") + "/api/projects/" + strings.TrimSpace(c.ProjectID) + "/chunks"
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		response, retryable, err := c.doUploadRequest(ctx, httpClient, url, payload)
		if err == nil {
			return response, nil
		}
		lastErr = err
		if !retryable || attempt == maxRetries-1 {
			break
		}

		backoff := time.Duration(1<<attempt) * 200 * time.Millisecond
		select {
		case <-ctx.Done():
			return UploadResponse{}, ctx.Err()
		case <-time.After(backoff):
		}
	}

	return UploadResponse{}, lastErr
}

func (c Client) doUploadRequest(ctx context.Context, httpClient *http.Client, url string, payload []byte) (UploadResponse, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return UploadResponse{}, false, fmt.Errorf("create upload request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(c.ProjectToken))
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return UploadResponse{}, true, fmt.Errorf("upload chunks: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return UploadResponse{}, resp.StatusCode >= 500, fmt.Errorf("read upload response: %w", err)
	}

	if resp.StatusCode >= 500 {
		return UploadResponse{}, true, fmt.Errorf("upload chunks failed with status %s", resp.Status)
	}
	if resp.StatusCode >= 400 {
		var apiErr apiErrorResponse
		if err := json.Unmarshal(body, &apiErr); err == nil && strings.TrimSpace(apiErr.Error) != "" {
			return UploadResponse{}, false, fmt.Errorf("upload chunks: %s", apiErr.Error)
		}
		return UploadResponse{}, false, fmt.Errorf("upload chunks failed with status %s", resp.Status)
	}

	var response UploadResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return UploadResponse{}, false, fmt.Errorf("decode upload response: %w", err)
	}
	return response, false, nil
}
