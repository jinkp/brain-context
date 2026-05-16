package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	// filepath used in runSetup via Abs

	"github.com/Gentleman-Programming/brain-context/internal/chunker"
	brainconfig "github.com/Gentleman-Programming/brain-context/internal/config"
	braincrypto "github.com/Gentleman-Programming/brain-context/internal/crypto"
	"github.com/Gentleman-Programming/brain-context/internal/embedder"
	"github.com/Gentleman-Programming/brain-context/internal/indexer"
	brainmcp "github.com/Gentleman-Programming/brain-context/internal/mcp"
	"github.com/Gentleman-Programming/brain-context/internal/parser"
	"github.com/Gentleman-Programming/brain-context/internal/scanner"
	braintui "github.com/Gentleman-Programming/brain-context/internal/tui"
	"github.com/Gentleman-Programming/brain-context/internal/uploader"
	brainversion "github.com/Gentleman-Programming/brain-context/internal/version"
	tea "github.com/charmbracelet/bubbletea"
)

var version = "v0.1.1" // overridden by -ldflags at build time

var httpClient = &http.Client{Timeout: 30 * time.Second}

type apiErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}

type authLoginRequest struct {
	APIKey string `json:"api_key"`
}

type createProjectRequest struct {
	Name            string `json:"name"`
	EmbedModel      string `json:"embed_model"`
	EmbedDimensions int    `json:"embed_dimensions"`
	EmbedAPIKey     string `json:"embed_api_key,omitempty"`
}

type createProjectResponse struct {
	ID      string                  `json:"id"`
	Project *createProjectResponse  `json:"project,omitempty"` // wrapped when embed_api_key sent
	Warning string                  `json:"warning,omitempty"`
}

type tokenEnvelope struct {
	Token string `json:"token"`
}

type projectTokensResponse struct {
	ProjectToken tokenEnvelope `json:"project_token"`
	MCPReadKey   tokenEnvelope `json:"mcp_read_key"`
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "login":
		err = runLogin(os.Args[2:])
	case "register":
		err = runRegister(os.Args[2:])
	case "index":
		err = runIndex(os.Args[2:])
	case "update":
		err = runUpdate(os.Args[2:])
	case "mcp":
		err = runMCP(os.Args[2:])
	case "projects":
		err = runProjects(os.Args[2:])
	case "tui":
		err = runSetup(os.Args[2:])
	case "setup": // backward compat alias for tui
		err = runSetup(os.Args[2:])
	case "tokens":
		err = runTokens(os.Args[2:])
	case "invite":
		err = runInvite(os.Args[2:])
	case "join":
		err = runJoin(os.Args[2:])
	case "version":
		fmt.Println("brain-context", version)
		// Check for update in background
		result := brainversion.CheckLatest(version)
		if result.Status == brainversion.StatusUpdateAvailable {
			fmt.Fprintf(os.Stderr, "\n🆕 Update available: %s → v%s\n   %s\n\n",
				version, result.LatestVersion, result.Message)
		}
		return
	default:
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func runLogin(args []string) error {
	fs := flag.NewFlagSet("login", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	token := fs.String("token", "", "tenant api key")
	apiEndpoint := fs.String("api", "http://localhost:8080", "api base url")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse login flags: %w", err)
	}
	if strings.TrimSpace(*token) == "" {
		return fmt.Errorf("--token is required")
	}
	endpoint := normalizeEndpoint(*apiEndpoint)
	if endpoint == "" {
		return fmt.Errorf("--api is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := verifyTenantToken(ctx, endpoint, strings.TrimSpace(*token)); err != nil {
		return err
	}

	cfg, err := brainconfig.Load()
	if err != nil {
		return err
	}
	cfg.APIEndpoint = endpoint
	cfg.TenantToken = strings.TrimSpace(*token)
	if err := brainconfig.Save(cfg); err != nil {
		return err
	}

	fmt.Println("Logged in successfully")
	return nil
}

func runRegister(args []string) error {
	fs := flag.NewFlagSet("register", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectName := fs.String("project", "", "project name")
	repoPath := fs.String("repo", "", "repository path")
	provider := fs.String("embedder", "", "embedder provider: gemini|openai|ollama")
	apiKey := fs.String("embed-api-key", "", "embedder api key (encrypted on server for team sharing)")
	model := fs.String("model", "", "embedder model")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse register flags: %w", err)
	}
	if strings.TrimSpace(*projectName) == "" {
		return fmt.Errorf("--project is required")
	}
	if err := validateRepoPath(*repoPath); err != nil {
		return err
	}
	if strings.TrimSpace(*provider) == "" {
		return fmt.Errorf("--embedder is required")
	}
	if strings.TrimSpace(*apiKey) == "" {
		return fmt.Errorf("--api-key is required")
	}
	if strings.TrimSpace(*model) == "" {
		return fmt.Errorf("--model is required")
	}

	fullModel := normalizeModel(*provider, *model)
	emb, err := embedder.New(fullModel, strings.TrimSpace(*apiKey))
	if err != nil {
		return err
	}

	cfg, err := brainconfig.Load()
	if err != nil {
		return err
	}
	if cfg.APIEndpoint == "" {
		return fmt.Errorf("api endpoint is not configured; run `brain login` first")
	}
	if cfg.TenantToken == "" {
		return fmt.Errorf("tenant token is not configured; run `brain login` first")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	project, err := createProject(ctx, cfg.APIEndpoint, cfg.TenantToken, createProjectRequest{
		Name:            strings.TrimSpace(*projectName),
		EmbedModel:      fullModel,
		EmbedDimensions: emb.Dimensions(),
		EmbedAPIKey:     strings.TrimSpace(*apiKey), // uploaded encrypted by the API
	})
	if err != nil {
		return err
	}

	tokens, err := createProjectTokens(ctx, cfg.APIEndpoint, cfg.TenantToken, project.ID)
	if err != nil {
		return err
	}

	cfg.Projects[strings.TrimSpace(*projectName)] = brainconfig.ProjectConfig{
		ProjectID:       project.ID,
		ProjectToken:    tokens.ProjectToken.Token,
		MCPReadKey:      tokens.MCPReadKey.Token,
		RepoPath:        mustAbsPath(*repoPath),
		EmbedModel:      fullModel,
		EmbedAPIKey:     strings.TrimSpace(*apiKey),
		EmbedDimensions: emb.Dimensions(),
	}
	if err := brainconfig.Save(cfg); err != nil {
		return err
	}

	fmt.Println("Project registered successfully")
	fmt.Println("WARNING: these tokens are shown once. Store them securely.")
	fmt.Println("project_id:", project.ID)
	fmt.Println("project_token:", tokens.ProjectToken.Token)
	fmt.Println("mcp_read_key:", tokens.MCPReadKey.Token)
	return nil
}

func verifyTenantToken(ctx context.Context, apiEndpoint, token string) error {
	payload, err := json.Marshal(authLoginRequest{APIKey: token})
	if err != nil {
		return fmt.Errorf("marshal auth request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiEndpoint+"/api/auth/login", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create auth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if _, err := doJSON(req, nil); err != nil {
		return fmt.Errorf("verify tenant token: %w", err)
	}
	return nil
}

func createProject(ctx context.Context, apiEndpoint, token string, body createProjectRequest) (createProjectResponse, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return createProjectResponse{}, fmt.Errorf("marshal create project request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiEndpoint+"/api/projects", bytes.NewReader(payload))
	if err != nil {
		return createProjectResponse{}, fmt.Errorf("create project request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	var response createProjectResponse
	if _, err := doJSON(req, &response); err != nil {
		return createProjectResponse{}, fmt.Errorf("register project: %w", err)
	}
	// Handle both response formats:
	// { "id": "..." }  — no embed_api_key
	// { "project": { "id": "..." }, "warning": "..." }  — with embed_api_key
	if strings.TrimSpace(response.ID) == "" && response.Project != nil {
		response.ID = response.Project.ID
	}
	if strings.TrimSpace(response.ID) == "" {
		return createProjectResponse{}, fmt.Errorf("register project: api returned empty project id")
	}
	if response.Warning != "" {
		fmt.Fprintf(os.Stderr, "⚠️  %s\n", response.Warning)
	}
	return response, nil
}

func createProjectTokens(ctx context.Context, apiEndpoint, token, projectID string) (projectTokensResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiEndpoint+"/api/projects/"+projectID+"/tokens", nil)
	if err != nil {
		return projectTokensResponse{}, fmt.Errorf("create project tokens request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	var response projectTokensResponse
	if _, err := doJSON(req, &response); err != nil {
		return projectTokensResponse{}, fmt.Errorf("issue project tokens: %w", err)
	}
	if strings.TrimSpace(response.ProjectToken.Token) == "" || strings.TrimSpace(response.MCPReadKey.Token) == "" {
		return projectTokensResponse{}, fmt.Errorf("issue project tokens: api returned empty tokens")
	}
	return response, nil
}

func doJSON(req *http.Request, out any) (*http.Response, error) {
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if resp.StatusCode >= 400 {
		var apiErr apiErrorResponse
		if err := json.Unmarshal(body, &apiErr); err == nil && strings.TrimSpace(apiErr.Error) != "" {
			return nil, errors.New(apiErr.Error)
		}
		return nil, fmt.Errorf("request failed with status %s", resp.Status)
	}
	if out == nil || len(body) == 0 {
		return resp, nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("decode response body: %w", err)
	}
	return resp, nil
}

func validateRepoPath(value string) error {
	path := strings.TrimSpace(value)
	if path == "" {
		return fmt.Errorf("--repo is required")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve repo path: %w", err)
	}
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("repo path does not exist: %s", absPath)
		}
		return fmt.Errorf("stat repo path: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("repo path must be a directory: %s", absPath)
	}
	return nil
}

func normalizeEndpoint(value string) string {
	return strings.TrimRight(strings.TrimSpace(value), "/")
}

func normalizeModel(provider, model string) string {
	provider = strings.TrimSpace(provider)
	model = strings.TrimSpace(model)
	if strings.Contains(model, "/") {
		return model
	}
	return provider + "/" + model
}

func runSetupTUI(clientsOnly bool) error {
	exe, err := os.Executable()
	if err != nil {
		exe = "brain"
	}
	m := braintui.New(exe, version, clientsOnly)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  brain login          --token <tenant-api-key> [--api http://localhost:8080]")
	fmt.Fprintln(os.Stderr, "  brain register       --project <name> --repo <path> --embedder <gemini|openai|ollama> --embed-api-key <key> --model <model>")
	fmt.Fprintln(os.Stderr, "  brain index          --project <name>")
	fmt.Fprintln(os.Stderr, "  brain update         --project <name>")
	fmt.Fprintln(os.Stderr, "  brain invite         --project <name>  [--ttl 24h]")
	fmt.Fprintln(os.Stderr, "  brain join           --code <brn_invite_xxx>  [--api http://localhost:8080]")
	fmt.Fprintln(os.Stderr, "  brain tokens list    --project <name>")
	fmt.Fprintln(os.Stderr, "  brain tokens renew   --project <name>")
	fmt.Fprintln(os.Stderr, "  brain projects                  list registered projects")
	fmt.Fprintln(os.Stderr, "  brain projects delete --project <name>  remove a project")
	fmt.Fprintln(os.Stderr, "  brain mcp            [--project <name>]")
	fmt.Fprintln(os.Stderr, "  brain tui                       interactive TUI (wizard, client setup, update)")
	fmt.Fprintln(os.Stderr, "  brain tui clients               TUI — client selection only")
	fmt.Fprintln(os.Stderr, "  brain tui <client>              direct: opencode, claude, cursor, gemini, windsurf, all")
	fmt.Fprintln(os.Stderr, "  brain version")
}

// ── invite ────────────────────────────────────────────────────────────────────

type createInviteResponse struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
	Message   string    `json:"message"`
}

func runInvite(args []string) error {
	fs := flag.NewFlagSet("invite", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectName := fs.String("project", "", "project name")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}
	if strings.TrimSpace(*projectName) == "" {
		return fmt.Errorf("--project is required")
	}

	_, projectID, tenantToken, apiEndpoint, err := loadProjectConfig(*projectName)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		apiEndpoint+"/api/projects/"+projectID+"/invite", nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tenantToken)

	var resp createInviteResponse
	if _, err := doJSON(req, &resp); err != nil {
		return fmt.Errorf("create invite: %w", err)
	}

	fmt.Println()
	fmt.Printf("  Invite code for project %q:\n\n", *projectName)
	fmt.Printf("  %s\n\n", resp.Code)
	fmt.Printf("  Expires: %s\n", resp.ExpiresAt.Local().Format("2006-01-02 15:04:05"))
	fmt.Println()
	fmt.Println("  Share with your developer. They run:")
	fmt.Printf("  brain join --code %s --api %s\n\n", resp.Code, apiEndpoint)
	return nil
}

// ── join ──────────────────────────────────────────────────────────────────────

type redeemInviteResponse struct {
	ProjectID       string `json:"project_id"`
	ProjectName     string `json:"project_name"`
	EmbedModel      string `json:"embed_model"`
	EmbedDimensions int    `json:"embed_dimensions"`
	EmbedAPIKeyEnc  string `json:"embed_api_key_enc"`
	MCPReadKey      string `json:"mcp_read_key"`
	Message         string `json:"message"`
}

func runJoin(args []string) error {
	fs := flag.NewFlagSet("join", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	code := fs.String("code", "", "invite code (brn_invite_...)")
	apiEndpoint := fs.String("api", "http://localhost:8080", "api base url")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}
	if strings.TrimSpace(*code) == "" {
		return fmt.Errorf("--code is required")
	}

	endpoint := normalizeEndpoint(*apiEndpoint)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Call redeem endpoint
	body, _ := json.Marshal(map[string]string{"code": strings.TrimSpace(*code)})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		endpoint+"/api/invite/redeem",
		bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	var resp redeemInviteResponse
	if _, err := doJSON(req, &resp); err != nil {
		return fmt.Errorf("redeem invite: %w", err)
	}

	// Save to local config
	cfg, err := brainconfig.Load()
	if err != nil {
		return err
	}
	cfg.APIEndpoint = endpoint

	// Keep existing project config if present, only update tokens + embed
	existing := cfg.Projects[resp.ProjectName]
	existing.ProjectID = resp.ProjectID
	existing.MCPReadKey = resp.MCPReadKey
	existing.EmbedModel = resp.EmbedModel
	existing.EmbedDimensions = resp.EmbedDimensions
	existing.EmbedAPIKeyEnc = resp.EmbedAPIKeyEnc
	cfg.Projects[resp.ProjectName] = existing

	if err := brainconfig.Save(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Println()
	fmt.Printf("  ✅ Joined project %q\n\n", resp.ProjectName)
	fmt.Printf("  embed_model:  %s\n", resp.EmbedModel)
	fmt.Printf("  mcp_read_key: %s\n", resp.MCPReadKey[:min(30, len(resp.MCPReadKey))]+"...")
	fmt.Println()
	fmt.Println("  Next steps:")
	fmt.Println("  brain tui              # interactive setup, or:")
	fmt.Println("  brain tui opencode     # or claude, cursor, gemini, windsurf, all")
	fmt.Println("  → Restart your IDE and the MCP tools will appear")
	fmt.Println()
	fmt.Println("  ⚠️  Store your mcp_read_key — it is shown once.")
	return nil
}

// ── tokens ───────────────────────────────────────────────────────────────────

type listTokensResponse struct {
	Tokens []tokenInfo `json:"tokens"`
}

type tokenInfo struct {
	ID        string    `json:"id"`
	Prefix    string    `json:"prefix"`
	Scope     string    `json:"scope"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

type renewTokensResponse struct {
	Message      string        `json:"message"`
	ProjectToken tokenEnvelope `json:"project_token"`
	MCPReadKey   tokenEnvelope `json:"mcp_read_key"`
}

func runTokens(args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage:")
		fmt.Fprintln(os.Stderr, "  brain tokens list  --project <name>")
		fmt.Fprintln(os.Stderr, "  brain tokens renew --project <name>")
		return fmt.Errorf("subcommand required: list or renew")
	}

	switch args[0] {
	case "list":
		return runTokensList(args[1:])
	case "renew":
		return runTokensRenew(args[1:])
	default:
		return fmt.Errorf("unknown subcommand %q — use list or renew", args[0])
	}
}

func runTokensList(args []string) error {
	fs := flag.NewFlagSet("tokens list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectName := fs.String("project", "", "project name")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}
	if strings.TrimSpace(*projectName) == "" {
		return fmt.Errorf("--project is required")
	}

	_, projectID, tenantToken, apiEndpoint, err := loadProjectConfig(*projectName)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		apiEndpoint+"/api/projects/"+projectID+"/tokens", nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tenantToken)

	var resp listTokensResponse
	if _, err := doJSON(req, &resp); err != nil {
		return fmt.Errorf("list tokens: %w", err)
	}

	if len(resp.Tokens) == 0 {
		fmt.Println("No active tokens found for project", *projectName)
		return nil
	}

	fmt.Printf("Active tokens for project %q:\n\n", *projectName)
	fmt.Printf("  %-12s  %-36s  %-10s  %s\n", "SCOPE", "ID", "PREFIX", "EXPIRES")
	fmt.Printf("  %-12s  %-36s  %-10s  %s\n",
		"────────────", "────────────────────────────────────",
		"──────────", "───────────────────────")
	for _, t := range resp.Tokens {
		fmt.Printf("  %-12s  %-36s  %-10s  %s\n",
			t.Scope, t.ID, t.Prefix[:min(len(t.Prefix), 20)]+"...",
			t.ExpiresAt.Format("2006-01-02"))
	}
	fmt.Println()
	return nil
}

func runTokensRenew(args []string) error {
	fs := flag.NewFlagSet("tokens renew", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectName := fs.String("project", "", "project name")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}
	if strings.TrimSpace(*projectName) == "" {
		return fmt.Errorf("--project is required")
	}

	cfg, projectID, tenantToken, apiEndpoint, err := loadProjectConfig(*projectName)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		apiEndpoint+"/api/projects/"+projectID+"/tokens/renew", nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+tenantToken)

	var resp renewTokensResponse
	if _, err := doJSON(req, &resp); err != nil {
		return fmt.Errorf("renew tokens: %w", err)
	}

	// Update config with new tokens
	existing := cfg.Projects[strings.TrimSpace(*projectName)]
	existing.ProjectToken = resp.ProjectToken.Token
	existing.MCPReadKey = resp.MCPReadKey.Token
	cfg.Projects[strings.TrimSpace(*projectName)] = existing
	if err := brainconfig.Save(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Println("⚠️  Previous tokens have been revoked.")
	fmt.Println("⚠️  Store these new tokens securely — shown once.")
	fmt.Println()
	fmt.Println("project_token:", resp.ProjectToken.Token)
	fmt.Println("mcp_read_key: ", resp.MCPReadKey.Token)
	fmt.Println()
	fmt.Println("✅ Config updated at ~/.brain/config.yaml")
	return nil
}

// loadProjectConfig is a helper shared by tokens subcommands.
func loadProjectConfig(projectName string) (cfg brainconfig.Config, projectID, tenantToken, apiEndpoint string, err error) {
	c, err := brainconfig.Load()
	if err != nil {
		return brainconfig.Config{}, "", "", "", err
	}
	if strings.TrimSpace(c.TenantToken) == "" {
		return brainconfig.Config{}, "", "", "", fmt.Errorf("not logged in — run `brain login` first")
	}
	if strings.TrimSpace(c.APIEndpoint) == "" {
		return brainconfig.Config{}, "", "", "", fmt.Errorf("api endpoint not set — run `brain login` first")
	}
	proj, ok := c.Projects[strings.TrimSpace(projectName)]
	if !ok {
		return brainconfig.Config{}, "", "", "", fmt.Errorf("project %q not found — run `brain register` first", projectName)
	}
	return c, proj.ProjectID, c.TenantToken, c.APIEndpoint, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ── projects ─────────────────────────────────────────────────────────────────

func runProjects(args []string) error {
	if len(args) > 0 && args[0] == "delete" {
		return runProjectsDelete(args[1:])
	}
	return runProjectsList()
}

func runProjectsList() error {
	cfg, err := brainconfig.Load()
	if err != nil {
		return err
	}

	if len(cfg.Projects) == 0 {
		fmt.Println("No projects registered. Run `brain register` to add one.")
		return nil
	}

	fmt.Printf("Registered projects (%d):\n\n", len(cfg.Projects))
	fmt.Printf("  %-20s  %-36s  %-20s  %s\n", "NAME", "ID", "LAST INDEXED", "EMBEDDER")
	fmt.Printf("  %-20s  %-36s  %-20s  %s\n",
		"────────────────────", "────────────────────────────────────",
		"────────────────────", "────────────────────────────")

	for name, project := range cfg.Projects {
		embedModel := project.EmbedModel
		if embedModel == "" {
			embedModel = "(not set)"
		}
		id := project.ProjectID
		if id == "" {
			id = "(pending)"
		}

		// Read last indexed date from local snapshot
		lastIndexed := "(never)"
		if id != "(pending)" {
			snapshot, snapErr := indexer.LoadSnapshot(id)
			if snapErr == nil && !snapshot.UpdatedAt.IsZero() {
				lastIndexed = snapshot.UpdatedAt.Local().Format("2006-01-02 15:04")
			}
		}

		fmt.Printf("  %-20s  %-36s  %-20s  %s\n", name, id, lastIndexed, embedModel)
	}
	fmt.Println()
	return nil
}

func runProjectsDelete(args []string) error {
	fs := flag.NewFlagSet("projects delete", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectName := fs.String("project", "", "project name to delete")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}
	if strings.TrimSpace(*projectName) == "" {
		return fmt.Errorf("--project is required")
	}

	cfg, err := brainconfig.Load()
	if err != nil {
		return err
	}

	project, ok := cfg.Projects[strings.TrimSpace(*projectName)]
	if !ok {
		return fmt.Errorf("project %q not found in local config", strings.TrimSpace(*projectName))
	}

	// 1. Try to delete from API (if admin has tenant token)
	if strings.TrimSpace(cfg.TenantToken) != "" && strings.TrimSpace(cfg.APIEndpoint) != "" && strings.TrimSpace(project.ProjectID) != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
			cfg.APIEndpoint+"/api/projects/"+project.ProjectID, nil)
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+cfg.TenantToken)
			resp, apiErr := httpClient.Do(req)
			if apiErr != nil {
				fmt.Fprintf(os.Stderr, "  ⚠️  API unreachable — removed from local config only\n")
			} else {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
					fmt.Printf("  ✅ Deleted from API\n")
				} else if resp.StatusCode == http.StatusNotFound {
					fmt.Printf("  ℹ️  Not found on API (already deleted or never synced)\n")
				} else {
					fmt.Fprintf(os.Stderr, "  ⚠️  API returned %s — removed from local config only\n", resp.Status)
				}
			}
		}
	}

	// 2. Remove local snapshot
	if strings.TrimSpace(project.ProjectID) != "" {
		snapshotDir, _ := os.UserHomeDir()
		if snapshotDir != "" {
			snapshotFile := filepath.Join(snapshotDir, ".brain", "snapshots", project.ProjectID+".json")
			if err := os.Remove(snapshotFile); err == nil {
				fmt.Printf("  ✅ Local snapshot removed\n")
			}
		}

		// Remove embedding cache
		cacheDir, _ := os.UserHomeDir()
		if cacheDir != "" {
			embedCacheDir := filepath.Join(cacheDir, ".brain", "cache", project.ProjectID)
			if err := os.RemoveAll(embedCacheDir); err == nil {
				fmt.Printf("  ✅ Embedding cache removed\n")
			}
		}
	}

	// 3. Remove from local config
	delete(cfg.Projects, strings.TrimSpace(*projectName))
	if err := brainconfig.Save(cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	fmt.Printf("  ✅ Project %q removed from local config\n", strings.TrimSpace(*projectName))
	fmt.Println()
	return nil
}

// ── setup ────────────────────────────────────────────────────────────────────

type mcpClientConfig struct {
	name        string
	description string
	configPaths []configPathFn       // ordered candidate locations
	merge       configMergeFn        // how to inject our entry
}

type configPathFn func() string
type configMergeFn func(path string) error

var supportedClients = []string{"opencode", "claude", "cursor", "gemini", "windsurf", "all"}

func runSetup(args []string) error {
	// No args → launch full TUI wizard
	if len(args) == 0 {
		return runSetupTUI(false)
	}
	// --clients → TUI skips to client selection only
	if args[0] == "--clients" || args[0] == "clients" {
		return runSetupTUI(true)
	}
	client := strings.ToLower(strings.TrimSpace(args[0]))

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}

	brainExe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}
	brainExe, _ = filepath.Abs(brainExe)

	var targets []string
	if client == "all" {
		targets = []string{"opencode", "claude", "cursor", "gemini", "windsurf"}
	} else {
		targets = []string{client}
	}

	anyOK := false
	for _, t := range targets {
		if err := braintui.SetupClient(t, home, brainExe); err != nil {
			fmt.Fprintf(os.Stderr, "  ⚠️  %s: %v\n", t, err)
		} else {
			fmt.Printf("  ✅ %s configured\n", t)
			anyOK = true
		}
	}

	if anyOK {
		fmt.Println()
		fmt.Println("✅ Done. Restart your IDE / agent to load brain-context MCP tools.")
		fmt.Println()
		fmt.Println("Available tools:")
		fmt.Println("  • search_project_context")
		fmt.Println("  • get_file_summary")
		fmt.Println("  • get_related_files")
		fmt.Println("  • explain_flow")
		fmt.Println("  • find_impact")
	}
	return nil
}

func runMCP(args []string) error {
	fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectName := fs.String("project", "", "default project name")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse mcp flags: %w", err)
	}

	srv, err := brainmcp.New(strings.TrimSpace(*projectName))
	if err != nil {
		return err
	}
	return srv.Start()
}

func runIndex(args []string) error {
	return runIndexCommand(args, false)
}

func runUpdate(args []string) error {
	return runIndexCommand(args, true)
}

func runIndexCommand(args []string, incremental bool) error {
	commandName := "index"
	if incremental {
		commandName = "update"
	}

	fs := flag.NewFlagSet(commandName, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	projectName := fs.String("project", "", "project name")
	stack := fs.String("stack", "", "stack filter: go, dotnet, react, vue, angular, node, python, java, rust, ruby, php")
	include := fs.String("include", "", "extra extensions to include, comma-separated (e.g. .proto,.graphql)")
	exclude := fs.String("exclude", "", "directories to exclude, comma-separated (e.g. assets,Migrations,Tests)")
	verbose := fs.Bool("verbose", false, "show each file as it's scanned")
	if err := fs.Parse(args); err != nil {
		return fmt.Errorf("parse %s flags: %w", commandName, err)
	}
	if strings.TrimSpace(*projectName) == "" {
		return fmt.Errorf("--project is required")
	}

	cfg, err := brainconfig.Load()
	if err != nil {
		return err
	}
	project, ok := cfg.Projects[strings.TrimSpace(*projectName)]
	if !ok {
		return fmt.Errorf("project %q is not registered; run `brain register` first", strings.TrimSpace(*projectName))
	}
	if strings.TrimSpace(project.RepoPath) == "" {
		return fmt.Errorf("project %q is missing repo_path; re-run `brain register` to store repository metadata", strings.TrimSpace(*projectName))
	}
	if strings.TrimSpace(cfg.APIEndpoint) == "" {
		return fmt.Errorf("api endpoint is not configured; run `brain login` first")
	}
	if strings.TrimSpace(project.ProjectToken) == "" {
		return fmt.Errorf("project %q is missing project_token; re-run `brain register` first", strings.TrimSpace(*projectName))
	}
	if err := validateRepoPath(project.RepoPath); err != nil {
		return err
	}
	startedAt := time.Now()
	if incremental {
		hasSnapshot, err := indexer.HasSnapshot(project.ProjectID)
		if err != nil {
			return err
		}
		if !hasSnapshot {
			return fmt.Errorf("project %q has no local snapshot; run `brain index --project %s` first", strings.TrimSpace(*projectName), strings.TrimSpace(*projectName))
		}
	}

	// Build scan options from flags
	scanOpts := scanner.ScanOptions{
		Stack:   strings.TrimSpace(*stack),
		Verbose: *verbose,
	}
	if *include != "" {
		for _, ext := range strings.Split(*include, ",") {
			scanOpts.Include = append(scanOpts.Include, strings.TrimSpace(ext))
		}
	}
	if *exclude != "" {
		for _, dir := range strings.Split(*exclude, ",") {
			scanOpts.Exclude = append(scanOpts.Exclude, strings.TrimSpace(dir))
		}
	}
	if scanOpts.Stack != "" {
		fmt.Printf("⏳ Scanning %s (stack: %s)...\n", project.RepoPath, scanOpts.Stack)
	} else {
		fmt.Printf("⏳ Scanning %s...\n", project.RepoPath)
	}
	if scanOpts.Verbose {
		scanOpts.OnFile = func(path string) {
			fmt.Printf("   📄 %s\n", path)
		}
	}
	files, chunksByFile, allChunks, err := buildProjectChunks(project.RepoPath, scanOpts)
	if err != nil {
		return err
	}
	fmt.Printf("   %d files scanned, %d chunks extracted\n", len(files), len(allChunks))

	diff := indexer.DiffResult{
		NewFiles:       []string{},
		ModifiedFiles:  []string{},
		DeletedFiles:   []string{},
		NewChunks:      []indexer.ChunkRef{},
		ModifiedChunks: []indexer.ChunkRef{},
		DeletedChunks:  []indexer.ChunkRef{},
	}
	chunksToEmbed := allChunks
	if incremental {
		snapshot, err := indexer.LoadSnapshot(project.ProjectID)
		if err != nil {
			return fmt.Errorf("load snapshot: %w", err)
		}
		diff = indexer.Diff(snapshot, files, chunksByFile)
		chunksToEmbed = filterChunksForDiff(chunksByFile, diff)
	} else {
		for _, file := range files {
			diff.NewFiles = append(diff.NewFiles, file.Path)
			for _, chunk := range chunksByFile[file.Path] {
				diff.NewChunks = append(diff.NewChunks, indexer.ChunkRef{FilePath: file.Path, ChunkHash: chunk.ChunkHash})
			}
		}
	}

	if len(chunksToEmbed) == 0 {
		fmt.Println("✅ No changes detected — index is up to date")
		return nil
	}
	fmt.Printf("⏳ Embedding %d chunks...\n", len(chunksToEmbed))
	embeddings, err := embedChunks(project, chunksToEmbed)
	if err != nil {
		return err
	}
	fmt.Println("   Embeddings generated")
	uploadPayloads, err := buildUploadPayloads(chunksToEmbed, embeddings)
	if err != nil {
		return err
	}

	fmt.Printf("⏳ Uploading to %s...\n", cfg.APIEndpoint)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	uploadResult, err := uploader.Client{
		BaseURL:      cfg.APIEndpoint,
		ProjectID:    project.ProjectID,
		ProjectToken: project.ProjectToken,
		HTTPClient:   httpClient,
	}.Upload(ctx, uploadPayloads)
	if err != nil {
		return err
	}
	fmt.Println("   Upload complete")
	if err := indexer.SaveSnapshot(project.ProjectID, files, chunksByFile); err != nil {
		return fmt.Errorf("save snapshot: %w", err)
	}

	localSkipped := len(allChunks) - len(chunksToEmbed)
	if localSkipped < 0 {
		localSkipped = 0
	}
	printIndexSummary(len(files), uploadResult.Accepted, localSkipped+uploadResult.Skipped, len(diff.DeletedChunks), time.Since(startedAt))
	return nil
}

func buildProjectChunks(repoPath string, opts ...scanner.ScanOptions) ([]scanner.ScannedFile, map[string][]chunker.Chunk, []chunker.Chunk, error) {
	files, err := scanner.Scan(repoPath, opts...)
	if err != nil {
		return nil, nil, nil, err
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].Path < files[j].Path
	})

	chunksByFile := make(map[string][]chunker.Chunk, len(files))
	allChunks := make([]chunker.Chunk, 0)
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(repoPath, filepath.FromSlash(file.Path)))
		if err != nil {
			return nil, nil, nil, fmt.Errorf("read scanned file %s: %w", file.Path, err)
		}
		symbols, err := parser.ParseFile(file.Path, file.Language, data)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("parse file %s: %w", file.Path, err)
		}
		if err := parser.ValidateSymbols(symbols); err != nil {
			return nil, nil, nil, fmt.Errorf("validate symbols for %s: %w", file.Path, err)
		}
		chunks, err := chunker.Build(file, symbols, data)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("chunk file %s: %w", file.Path, err)
		}
		chunksByFile[file.Path] = chunks
		allChunks = append(allChunks, chunks...)
	}

	return files, chunksByFile, allChunks, nil
}

func filterChunksForDiff(chunksByFile map[string][]chunker.Chunk, diff indexer.DiffResult) []chunker.Chunk {
	targets := make(map[string]map[string]bool)
	for _, ref := range diff.NewChunks {
		if targets[ref.FilePath] == nil {
			targets[ref.FilePath] = map[string]bool{}
		}
		targets[ref.FilePath][ref.ChunkHash] = true
	}
	for _, ref := range diff.ModifiedChunks {
		if targets[ref.FilePath] == nil {
			targets[ref.FilePath] = map[string]bool{}
		}
		targets[ref.FilePath][ref.ChunkHash] = true
	}

	chunks := make([]chunker.Chunk, 0)
	for filePath, chunkHashes := range targets {
		for _, chunk := range chunksByFile[filePath] {
			if chunkHashes[chunk.ChunkHash] {
				chunks = append(chunks, chunk)
			}
		}
	}
	return chunks
}

func resolveEmbedAPIKey(project brainconfig.ProjectConfig) (string, error) {
	// Plaintext key (admin local config) takes precedence
	if strings.TrimSpace(project.EmbedAPIKey) != "" {
		return strings.TrimSpace(project.EmbedAPIKey), nil
	}
	// Encrypted key (from brain join) — decrypt in memory
	if strings.TrimSpace(project.EmbedAPIKeyEnc) != "" {
		key, err := braincrypto.Decrypt(strings.TrimSpace(project.EmbedAPIKeyEnc))
		if err != nil {
			return "", fmt.Errorf("decrypt embed api key: %w — make sure BRAIN_ENCRYPTION_KEY matches the server", err)
		}
		return key, nil
	}
	return "", nil
}

func embedChunks(project brainconfig.ProjectConfig, chunks []chunker.Chunk) ([][]float32, error) {
	if len(chunks) == 0 {
		return [][]float32{}, nil
	}
	if strings.TrimSpace(project.EmbedModel) == "" {
		return nil, fmt.Errorf("project embed_model is missing")
	}

	apiKey, err := resolveEmbedAPIKey(project)
	if err != nil {
		return nil, err
	}
	if apiKey == "" && !strings.HasPrefix(project.EmbedModel, embedder.ProviderOllama+"/") {
		return nil, fmt.Errorf("embed api key is missing — run `brain join` or `brain register --embed-api-key <key>`")
	}

	// Load embedding cache
	cache, err := embedder.NewCache(project.ProjectID)
	if err != nil {
		// Non-fatal — continue without cache
		cache = nil
	}

	// Separate cached vs uncached chunks
	results := make([][]float32, len(chunks))
	uncachedIdxs := make([]int, 0)
	uncachedTexts := make([]string, 0)
	cacheHits := 0

	for i, chunk := range chunks {
		if cache != nil {
			if cached := cache.Get(chunk.ChunkHash); cached != nil {
				results[i] = cached
				cacheHits++
				continue
			}
		}
		uncachedIdxs = append(uncachedIdxs, i)
		uncachedTexts = append(uncachedTexts, chunk.Content)
	}

	if cacheHits > 0 {
		fmt.Printf("   ⚡ %d/%d from cache, %d to embed\n", cacheHits, len(chunks), len(uncachedTexts))
	}

	if len(uncachedTexts) == 0 {
		return results, nil
	}

	emb, err := embedder.New(project.EmbedModel, apiKey)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	vectors, embedErr := emb.Embed(ctx, uncachedTexts)

	// Cache whatever we got — even partial results
	for j := 0; j < len(vectors); j++ {
		if j < len(uncachedIdxs) {
			results[uncachedIdxs[j]] = vectors[j]
			if cache != nil {
				cache.Set(chunks[uncachedIdxs[j]].ChunkHash, vectors[j])
			}
		}
	}

	// Save cache to disk — even on error, preserves progress
	if cache != nil {
		if saveErr := cache.Save(); saveErr != nil {
			fmt.Fprintf(os.Stderr, "⚠️  cache save failed: %v\n", saveErr)
		} else if len(vectors) > 0 {
			fmt.Printf("   💾 %d embeddings cached to disk\n", cache.Len())
		}
	}

	if embedErr != nil {
		return nil, fmt.Errorf("embed chunks: %w\n   Re-run the same command — cached embeddings will be reused", embedErr)
	}

	return results, nil
}

func buildUploadPayloads(chunks []chunker.Chunk, embeddings [][]float32) ([]uploader.ChunkPayload, error) {
	if len(chunks) != len(embeddings) {
		return nil, fmt.Errorf("chunk and embedding counts do not match")
	}
	payloads := make([]uploader.ChunkPayload, 0, len(chunks))
	for idx, chunk := range chunks {
		payloads = append(payloads, uploader.ChunkPayload{
			ChunkHash:  chunk.ChunkHash,
			SymbolName: chunk.SymbolName,
			SymbolType: chunk.SymbolType,
			StartLine:  chunk.StartLine,
			EndLine:    chunk.EndLine,
			FilePath:   chunk.FilePath,
			Language:   chunk.Language,
			TokenCount: chunk.TokenCount,
			Embedding:  embeddings[idx],
		})
	}
	return payloads, nil
}

func printIndexSummary(scanned, uploaded, skipped, deleted int, duration time.Duration) {
	fmt.Printf("Scanned: %d files\n", scanned)
	fmt.Printf("Uploaded: %d new chunks\n", uploaded)
	fmt.Printf("Skipped: %d unchanged chunks\n", skipped)
	fmt.Printf("Deleted: %d chunks\n", deleted)
	fmt.Printf("Index updated in %.1fs\n", duration.Seconds())
}

func mustAbsPath(path string) string {
	absPath, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return strings.TrimSpace(path)
	}
	return absPath
}
