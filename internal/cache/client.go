package cache

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

// projectStateResponse is the wire format returned by GET /api/projects/:id/state.
type projectStateResponse struct {
	ProjectID       string `json:"project_id"`
	ProjectName     string `json:"project_name"`
	LastIndexed     string `json:"last_indexed"`      // RFC3339 or empty
	EmbedModel      string `json:"embed_model"`
	EmbedDimensions int    `json:"embed_dimensions"`
}

// FetchProjectState retrieves fresh project metadata from the API.
// It calls GET {apiEndpoint}/api/projects/{projectID}/state and returns
// a ProjectState ready for freshness comparison and caching.
//
// Returns an error only on network or decode failures; a 404 is treated as
// "project not found on server" and also returns an error.
func FetchProjectState(ctx context.Context, httpClient *http.Client, apiEndpoint, projectID, mcpReadKey string) (ProjectState, error) {
	endpoint := fmt.Sprintf("%s/api/projects/%s/state",
		strings.TrimRight(apiEndpoint, "/"),
		projectID,
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return ProjectState{}, fmt.Errorf("build state request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(mcpReadKey))

	resp, err := httpClient.Do(req)
	if err != nil {
		return ProjectState{}, fmt.Errorf("fetch project state: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ProjectState{}, fmt.Errorf("read project state response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return ProjectState{}, fmt.Errorf("project %q not found on server", projectID)
	}
	if resp.StatusCode >= 400 {
		// Try to extract API error message.
		var apiErr struct {
			Error string `json:"error"`
		}
		if jsonErr := json.Unmarshal(body, &apiErr); jsonErr == nil && apiErr.Error != "" {
			return ProjectState{}, fmt.Errorf("project state API error: %s", apiErr.Error)
		}
		return ProjectState{}, fmt.Errorf("project state returned status %s", resp.Status)
	}

	var wire projectStateResponse
	if err := json.NewDecoder(bytes.NewReader(body)).Decode(&wire); err != nil {
		return ProjectState{}, fmt.Errorf("decode project state: %w", err)
	}

	return ProjectState{
		ProjectID:       wire.ProjectID,
		ProjectName:     wire.ProjectName,
		LastIndexed:     wire.LastIndexed,
		EmbedModel:      wire.EmbedModel,
		EmbedDimensions: wire.EmbedDimensions,
		APIEndpoint:     apiEndpoint,
		UpdatedAt:       time.Now().UTC(),
	}, nil
}
