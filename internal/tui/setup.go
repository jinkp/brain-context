package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Gentleman-Programming/brain-context/internal/config"
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

	// 1. Inject MCP entry
	mcp := getOrCreateMap(cfg, "mcp")
	mcp["brain-context"] = map[string]any{
		"type":    "local",
		"command": mcpCommandEntry(brainExe),
	}
	cfg["mcp"] = mcp

	if err := writeJSONMap(path, cfg); err != nil {
		return err
	}

	// 2. Inject protocol into the prompt file referenced by an agent
	configDir := filepath.Dir(path)
	promptFile := findOpenCodePromptFile(cfg, configDir)
	if promptFile == "" {
		// No agent references a {file:...} prompt — create AGENTS.md next to opencode.json
		promptFile = filepath.Join(configDir, "AGENTS.md")
	}

	return injectProtocolIntoFile(promptFile)
}

// ── Claude Code ───────────────────────────────────────────────────────────────

// claudeDotPath returns the canonical user-scope Claude config file path.
func claudeDotPath(home string) string {
	return filepath.Join(home, ".claude.json")
}

// setupClaude configures Claude Code at user scope: writes to ~/.claude.json
// (the canonical MCP target per official docs), injects the protocol into
// ~/.claude/CLAUDE.md, and migrates any stale entry from ~/.claude/settings.json.
func setupClaude(home, brainExe string) error {
	// 1. Merge MCP entry into ~/.claude.json
	clodotPath := claudeDotPath(home)

	cfg, err := readJSONMap(clodotPath)
	if err != nil {
		if !os.IsNotExist(err) {
			// File exists but is invalid JSON — abort without writing
			return fmt.Errorf("read %s: %w", clodotPath, err)
		}
		cfg = map[string]any{}
	}

	mcpServers := getOrCreateMap(cfg, "mcpServers")
	mcpServers["brain-context"] = map[string]any{
		"type":    "stdio",
		"command": brainExe,
		"args":    []string{"mcp"},
	}
	cfg["mcpServers"] = mcpServers

	if err := writeJSONMap(clodotPath, cfg); err != nil {
		return err
	}

	// 2. Best-effort migration: remove stale entry from ~/.claude/settings.json
	if err := migrateOldSettings(home); err != nil {
		fmt.Fprintf(os.Stderr, "  ⚠️  settings.json migration skipped: %v\n", err)
	}

	// 3. Inject protocol into ~/.claude/CLAUDE.md
	claudeMD := filepath.Join(home, ".claude", "CLAUDE.md")
	return injectProtocolIntoFile(claudeMD)
}

// migrateOldSettings removes the brain-context entry from ~/.claude/settings.json
// if present. Best-effort — errors are returned but callers should not fail on them.
func migrateOldSettings(home string) error {
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // file doesn't exist — no-op
		}
		return fmt.Errorf("read settings.json: %w", err)
	}

	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse settings.json: %w", err)
	}

	mcpServers, ok := cfg["mcpServers"].(map[string]any)
	if !ok {
		return nil // no mcpServers key — no-op
	}
	if _, has := mcpServers["brain-context"]; !has {
		return nil // key not present — no-op
	}

	delete(mcpServers, "brain-context")
	cfg["mcpServers"] = mcpServers
	return writeJSONMap(settingsPath, cfg)
}

// setupClaudeLocal configures Claude Code at local scope: for each registered
// brain-context project, writes .mcp.json in the project repo root with a
// --project <name> arg so Claude Code scopes the MCP server to that project.
func setupClaudeLocal(home, brainExe string) error {
	brainCfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load brain config: %w", err)
	}
	return setupClaudeLocalWithConfig(brainCfg, brainExe)
}

// setupClaudeLocalWithConfig is the testable core of setupClaudeLocal.
// It accepts an explicit config.Config so tests can inject temp-dir projects
// without touching the real ~/.brain/config.yaml.
func setupClaudeLocalWithConfig(brainCfg config.Config, brainExe string) error {
	for name, proj := range brainCfg.Projects {
		repoPath := strings.TrimSpace(proj.RepoPath)
		if repoPath == "" {
			continue
		}

		// 1. Write .mcp.json
		mcpPath := filepath.Join(repoPath, ".mcp.json")
		mcpCfg, err := readJSONMap(mcpPath)
		if err != nil {
			if !os.IsNotExist(err) {
				fmt.Fprintf(os.Stderr, "  ⚠️  claude local: parse %s: %v\n", mcpPath, err)
				continue
			}
			mcpCfg = map[string]any{}
		}

		mcpServers := getOrCreateMap(mcpCfg, "mcpServers")
		mcpServers["brain-context"] = map[string]any{
			"command": brainExe,
			"args":    []string{"mcp", "--project", name},
		}
		mcpCfg["mcpServers"] = mcpServers

		if err := writeJSONMap(mcpPath, mcpCfg); err != nil {
			fmt.Fprintf(os.Stderr, "  ⚠️  claude local: write %s: %v\n", mcpPath, err)
			continue
		}

		// 2. Inject protocol into <repo>/CLAUDE.md
		claudeMD := filepath.Join(repoPath, "CLAUDE.md")
		if err := injectProtocolIntoFile(claudeMD); err != nil {
			fmt.Fprintf(os.Stderr, "  ⚠️  claude local: protocol injection skipped for %s: %v\n", repoPath, err)
		}
	}

	return nil
}

// SetupClientLocal is the exported entry point for local-scope setup.
// For "claude" it calls setupClaudeLocal; all other clients fall through
// to the standard user-scope setup (local scope only differs for Claude Code).
func SetupClientLocal(client, home, brainExe string) error {
	switch client {
	case "claude":
		return setupClaudeLocal(home, brainExe)
	default:
		return setupClient(client, home, brainExe)
	}
}

// ── Cursor ────────────────────────────────────────────────────────────────────

func setupCursor(home, brainExe string) error {
	// 1. Inject MCP entry into mcp.json
	candidates := []string{
		filepath.Join(home, ".cursor", "mcp.json"),
		filepath.Join(home, "Library", "Application Support", "Cursor", "mcp.json"),
		filepath.Join(os.Getenv("APPDATA"), "Cursor", "mcp.json"),
	}
	path := firstExisting(candidates)
	if path == "" {
		path = candidates[0]
	}

	mcpCfg, err := readJSONMap(path)
	if err != nil {
		mcpCfg = map[string]any{}
	}

	mcpServers := getOrCreateMap(mcpCfg, "mcpServers")
	mcpServers["brain-context"] = map[string]any{
		"command": brainExe,
		"args":    []string{"mcp"},
	}
	mcpCfg["mcpServers"] = mcpServers

	if err := writeJSONMap(path, mcpCfg); err != nil {
		return err
	}

	// 2. Inject protocol into .cursor/rules/brain-context.md for each registered project
	brainCfg, err := config.Load()
	if err != nil {
		// Config not available — skip protocol injection, MCP is already registered
		return nil
	}
	for _, proj := range brainCfg.Projects {
		repoPath := strings.TrimSpace(proj.RepoPath)
		if repoPath == "" {
			continue
		}
		rulesDir := filepath.Join(repoPath, ".cursor", "rules")
		rulesFile := filepath.Join(rulesDir, "brain-context.md")
		if err := injectProtocolIntoFile(rulesFile); err != nil {
			// Log but don't fail — MCP setup succeeded, protocol is best-effort
			fmt.Fprintf(os.Stderr, "  ⚠️  cursor rules injection skipped for %s: %v\n", repoPath, err)
		}
	}

	return nil
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

// ── Protocol injection helpers ───────────────────────────────────────────────

// findOpenCodePromptFile looks through the agent configs in opencode.json
// for the first primary agent with a {file:...} prompt reference and returns
// the resolved absolute path. Returns "" if none found.
func findOpenCodePromptFile(cfg map[string]any, configDir string) string {
	agents, ok := cfg["agent"]
	if !ok {
		return ""
	}
	agentMap, ok := agents.(map[string]any)
	if !ok {
		return ""
	}

	// Look for the first primary agent with a file-referenced prompt
	for _, agentCfg := range agentMap {
		agent, ok := agentCfg.(map[string]any)
		if !ok {
			continue
		}
		prompt, ok := agent["prompt"].(string)
		if !ok {
			continue
		}
		// Match {file:./path} or {file:path}
		if !strings.HasPrefix(prompt, "{file:") || !strings.HasSuffix(prompt, "}") {
			continue
		}
		relPath := strings.TrimSuffix(strings.TrimPrefix(prompt, "{file:"), "}")
		relPath = strings.TrimSpace(relPath)
		if relPath == "" {
			continue
		}
		// Resolve relative to config directory
		if !filepath.IsAbs(relPath) {
			return filepath.Join(configDir, relPath)
		}
		return relPath
	}
	return ""
}

// injectProtocolIntoFile injects or updates the brain-context protocol block
// in a markdown/text file using marker comments. Non-destructive:
//   - If markers exist → replaces ONLY the content between them
//   - If markers don't exist → appends the block at the end
//   - If file doesn't exist → creates it with the block
//   - All other content is preserved untouched
func injectProtocolIntoFile(path string) error {
	protocol := wrappedProtocol()

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// File doesn't exist — create it with just the protocol
			if mkErr := os.MkdirAll(filepath.Dir(path), 0755); mkErr != nil {
				return fmt.Errorf("create directory for %s: %w", path, mkErr)
			}
			return os.WriteFile(path, []byte(protocol+"\n"), 0644)
		}
		return fmt.Errorf("read %s: %w", path, err)
	}

	content := string(data)
	startIdx := strings.Index(content, markerStart)
	endIdx := strings.Index(content, markerEnd)

	if startIdx >= 0 && endIdx >= 0 && endIdx > startIdx {
		// Markers exist — replace the section between them (inclusive)
		updated := content[:startIdx] + protocol + content[endIdx+len(markerEnd):]
		return os.WriteFile(path, []byte(updated), 0644)
	}

	// No markers found — append at the end, separated by a blank line
	separator := "\n\n"
	trimmed := strings.TrimRight(content, " \t\r\n")
	if trimmed == "" {
		separator = ""
	}
	updated := trimmed + separator + protocol + "\n"
	return os.WriteFile(path, []byte(updated), 0644)
}
