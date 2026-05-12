package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// setupClient configures the MCP entry for the given client.
// This is called both from the TUI (doSetupClients) and from
// the CLI direct mode (brain setup <client>).
func setupClient(client, home, brainExe string) error {
	switch client {
	case "opencode":
		return setupOpenCode(home, brainExe)
	case "claude":
		return setupClaude(home, brainExe)
	case "cursor":
		return setupCursor(home, brainExe)
	case "gemini":
		return setupGemini(home, brainExe)
	case "windsurf":
		return setupWindsurf(home, brainExe)
	default:
		return fmt.Errorf("unknown client %q", client)
	}
}

// SetupClient is the exported entry point used by cmd/brain/main.go.
func SetupClient(client, home, brainExe string) error {
	return setupClient(client, home, brainExe)
}

func mcpCommandEntry(brainExe string) []string {
	return []string{brainExe, "mcp"}
}

// ── OpenCode ──────────────────────────────────────────────────────────────────

func setupOpenCode(home, brainExe string) error {
	candidates := []string{
		filepath.Join(home, ".config", "opencode", "opencode.json"),
		filepath.Join(home, "Library", "Application Support", "opencode", "opencode.json"),
	}
	path := firstExisting(candidates)
	if path == "" {
		path = candidates[0]
	}

	cfg, err := readJSONMap(path)
	if err != nil {
		cfg = map[string]any{}
	}

	mcp := getOrCreateMap(cfg, "mcp")
	mcp["brain-context"] = map[string]any{
		"type":    "local",
		"command": mcpCommandEntry(brainExe),
	}
	cfg["mcp"] = mcp

	return writeJSONMap(path, cfg)
}

// ── Claude Code ───────────────────────────────────────────────────────────────

func setupClaude(home, brainExe string) error {
	candidates := []string{
		filepath.Join(home, ".claude", "settings.json"),
		filepath.Join(home, "Library", "Application Support", "Claude", "settings.json"),
		filepath.Join(os.Getenv("APPDATA"), "Claude", "settings.json"),
	}
	path := firstExisting(candidates)
	if path == "" {
		path = candidates[0]
	}

	cfg, err := readJSONMap(path)
	if err != nil {
		cfg = map[string]any{}
	}

	mcpServers := getOrCreateMap(cfg, "mcpServers")
	mcpServers["brain-context"] = map[string]any{
		"command": brainExe,
		"args":    []string{"mcp"},
	}
	cfg["mcpServers"] = mcpServers

	return writeJSONMap(path, cfg)
}

// ── Cursor ────────────────────────────────────────────────────────────────────

func setupCursor(home, brainExe string) error {
	candidates := []string{
		filepath.Join(home, ".cursor", "mcp.json"),
		filepath.Join(home, "Library", "Application Support", "Cursor", "mcp.json"),
		filepath.Join(os.Getenv("APPDATA"), "Cursor", "mcp.json"),
	}
	path := firstExisting(candidates)
	if path == "" {
		path = candidates[0]
	}

	cfg, err := readJSONMap(path)
	if err != nil {
		cfg = map[string]any{}
	}

	mcpServers := getOrCreateMap(cfg, "mcpServers")
	mcpServers["brain-context"] = map[string]any{
		"command": brainExe,
		"args":    []string{"mcp"},
	}
	cfg["mcpServers"] = mcpServers

	return writeJSONMap(path, cfg)
}

// ── Gemini CLI ────────────────────────────────────────────────────────────────

func setupGemini(home, brainExe string) error {
	candidates := []string{
		filepath.Join(home, ".gemini", "settings.json"),
		filepath.Join(home, ".config", "gemini", "settings.json"),
	}
	path := firstExisting(candidates)
	if path == "" {
		path = candidates[0]
	}

	cfg, err := readJSONMap(path)
	if err != nil {
		cfg = map[string]any{}
	}

	mcpServers := getOrCreateMap(cfg, "mcpServers")
	mcpServers["brain-context"] = map[string]any{
		"command": brainExe,
		"args":    []string{"mcp"},
	}
	cfg["mcpServers"] = mcpServers

	return writeJSONMap(path, cfg)
}

// ── Windsurf ──────────────────────────────────────────────────────────────────

func setupWindsurf(home, brainExe string) error {
	candidates := []string{
		filepath.Join(home, ".codeium", "windsurf", "mcp_config.json"),
		filepath.Join(home, "Library", "Application Support", "Windsurf", "mcp_config.json"),
		filepath.Join(os.Getenv("APPDATA"), "Windsurf", "mcp_config.json"),
	}
	path := firstExisting(candidates)
	if path == "" {
		path = candidates[0]
	}

	cfg, err := readJSONMap(path)
	if err != nil {
		cfg = map[string]any{}
	}

	mcpServers := getOrCreateMap(cfg, "mcpServers")
	mcpServers["brain-context"] = map[string]any{
		"command": brainExe,
		"args":    []string{"mcp"},
	}
	cfg["mcpServers"] = mcpServers

	return writeJSONMap(path, cfg)
}

// ── JSON helpers ──────────────────────────────────────────────────────────────

func readJSONMap(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func writeJSONMap(path string, m map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

func getOrCreateMap(parent map[string]any, key string) map[string]any {
	if v, ok := parent[key]; ok {
		if m, ok := v.(map[string]any); ok {
			return m
		}
	}
	return map[string]any{}
}

func firstExisting(paths []string) string {
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}
