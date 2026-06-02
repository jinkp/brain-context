package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestBackwardCompatibility_OldConfigLoads(t *testing.T) {
	// Simulates a config.yaml from a previous version that has NO
	// exclude/include fields. Must unmarshal without error and leave
	// the new fields nil.
	const oldYAML = `api_endpoint: https://api.brain-context.dev
tenant_token: tok-abc123
projects:
  myapp:
    project_id: proj-001
    project_token: pt-xyz
    mcp_read_key: rk-111
    repo_path: /home/user/myapp
    embed_model: text-embedding-3-small
    embed_dimensions: 1024
`

	var cfg Config
	if err := yaml.Unmarshal([]byte(oldYAML), &cfg); err != nil {
		t.Fatalf("unmarshal old config: %v", err)
	}
	normalize(&cfg)
	if err := Validate(cfg); err != nil {
		t.Fatalf("validate old config: %v", err)
	}

	// Global exclude/include must be nil
	if cfg.Exclude != nil {
		t.Errorf("global Exclude = %v, want nil", cfg.Exclude)
	}
	if cfg.Include != nil {
		t.Errorf("global Include = %v, want nil", cfg.Include)
	}

	// Per-project exclude/include must be nil
	proj := cfg.Projects["myapp"]
	if proj.Exclude != nil {
		t.Errorf("project Exclude = %v, want nil", proj.Exclude)
	}
	if proj.Include != nil {
		t.Errorf("project Include = %v, want nil", proj.Include)
	}

	// Original fields must be preserved
	if cfg.APIEndpoint != "https://api.brain-context.dev" {
		t.Errorf("APIEndpoint = %q", cfg.APIEndpoint)
	}
	if proj.ProjectID != "proj-001" {
		t.Errorf("ProjectID = %q", proj.ProjectID)
	}
	if proj.EmbedDimensions != 1024 {
		t.Errorf("EmbedDimensions = %d", proj.EmbedDimensions)
	}
}

func TestBackwardCompatibility_RoundTripPreservesOldFormat(t *testing.T) {
	// Load old YAML -> Save -> the output must NOT contain exclude/include keys.
	const oldYAML = `api_endpoint: https://api.brain-context.dev
tenant_token: tok-abc123
projects:
  myapp:
    project_id: proj-001
    project_token: pt-xyz
    mcp_read_key: rk-111
    repo_path: /home/user/myapp
    embed_model: text-embedding-3-small
    embed_dimensions: 1024
`

	var cfg Config
	if err := yaml.Unmarshal([]byte(oldYAML), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	normalize(&cfg)

	out, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	output := string(out)

	// omitempty must suppress the new fields entirely
	if strings.Contains(output, "exclude:") {
		t.Errorf("output contains 'exclude:' but should not:\n%s", output)
	}
	if strings.Contains(output, "include:") {
		t.Errorf("output contains 'include:' but should not:\n%s", output)
	}

	// Original fields must still be present
	if !strings.Contains(output, "api_endpoint:") {
		t.Errorf("output missing api_endpoint:\n%s", output)
	}
	if !strings.Contains(output, "project_id:") {
		t.Errorf("output missing project_id:\n%s", output)
	}
}

func TestNewConfig_WithExcludeInclude(t *testing.T) {
	// A config with the new fields must round-trip correctly.
	const newYAML = `api_endpoint: https://api.brain-context.dev
tenant_token: tok-abc123
exclude:
  - Migrations
  - assets
include:
  - .proto
projects:
  myapp:
    project_id: proj-001
    project_token: pt-xyz
    mcp_read_key: rk-111
    repo_path: /home/user/myapp
    embed_model: text-embedding-3-small
    embed_dimensions: 1024
    exclude:
      - Tests
    include:
      - .graphql
`

	var cfg Config
	if err := yaml.Unmarshal([]byte(newYAML), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	normalize(&cfg)
	if err := Validate(cfg); err != nil {
		t.Fatalf("validate: %v", err)
	}

	// Global
	wantGlobalExclude := []string{"Migrations", "assets"}
	wantGlobalInclude := []string{".proto"}
	if !slicesEqual(cfg.Exclude, wantGlobalExclude) {
		t.Errorf("global Exclude = %v, want %v", cfg.Exclude, wantGlobalExclude)
	}
	if !slicesEqual(cfg.Include, wantGlobalInclude) {
		t.Errorf("global Include = %v, want %v", cfg.Include, wantGlobalInclude)
	}

	// Per-project
	proj := cfg.Projects["myapp"]
	wantProjExclude := []string{"Tests"}
	wantProjInclude := []string{".graphql"}
	if !slicesEqual(proj.Exclude, wantProjExclude) {
		t.Errorf("project Exclude = %v, want %v", proj.Exclude, wantProjExclude)
	}
	if !slicesEqual(proj.Include, wantProjInclude) {
		t.Errorf("project Include = %v, want %v", proj.Include, wantProjInclude)
	}

	// Round-trip: marshal and verify the new fields appear
	out, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	output := string(out)

	for _, key := range []string{"exclude:", "include:", "- Migrations", "- assets", "- .proto", "- Tests", "- .graphql"} {
		if !strings.Contains(output, key) {
			t.Errorf("output missing %q:\n%s", key, output)
		}
	}
}

func TestMixedConfig_OnlyGlobalExclude(t *testing.T) {
	// Only global exclude set, no project-level. Ensures omitempty
	// works per-field, not all-or-nothing.
	const mixedYAML = `api_endpoint: https://api.brain-context.dev
tenant_token: tok-abc123
exclude:
  - vendor
projects:
  myapp:
    project_id: proj-001
    project_token: pt-xyz
    embed_dimensions: 768
`

	var cfg Config
	if err := yaml.Unmarshal([]byte(mixedYAML), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	normalize(&cfg)
	if err := Validate(cfg); err != nil {
		t.Fatalf("validate: %v", err)
	}

	if !slicesEqual(cfg.Exclude, []string{"vendor"}) {
		t.Errorf("global Exclude = %v, want [vendor]", cfg.Exclude)
	}
	if cfg.Include != nil {
		t.Errorf("global Include = %v, want nil", cfg.Include)
	}

	proj := cfg.Projects["myapp"]
	if proj.Exclude != nil {
		t.Errorf("project Exclude = %v, want nil", proj.Exclude)
	}
	if proj.Include != nil {
		t.Errorf("project Include = %v, want nil", proj.Include)
	}
}

func slicesEqual(a, b []string) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ── Solution validation tests ─────────────────────────────────────────────────

func makeTestProjects() map[string]ProjectConfig {
	return map[string]ProjectConfig{
		"pagos": {
			ProjectID:       "proj-001",
			EmbedModel:      "openai/text-embedding-3-large",
			EmbedDimensions: 3072,
		},
		"portal": {
			ProjectID:       "proj-002",
			EmbedModel:      "openai/text-embedding-3-large",
			EmbedDimensions: 3072,
		},
		"other": {
			ProjectID:       "proj-003",
			EmbedModel:      "ollama/nomic-embed-text",
			EmbedDimensions: 768,
		},
		"other-dims": {
			ProjectID:       "proj-004",
			EmbedModel:      "openai/text-embedding-3-large",
			EmbedDimensions: 1024,
		},
	}
}

func TestSolutionValidation_CompatibleProjects(t *testing.T) {
	cfg := Config{
		Projects: makeTestProjects(),
		Solutions: map[string]SolutionConfig{
			"mi-suite": {Projects: []string{"pagos", "portal"}},
		},
	}
	if err := Validate(cfg); err != nil {
		t.Errorf("expected no error for compatible projects, got: %v", err)
	}
}

func TestSolutionValidation_IncompatibleModel(t *testing.T) {
	cfg := Config{
		Projects: makeTestProjects(),
		Solutions: map[string]SolutionConfig{
			"mi-suite": {Projects: []string{"pagos", "other"}},
		},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for incompatible embed models, got nil")
	}
	if !strings.Contains(err.Error(), "embed model mismatch") {
		t.Errorf("expected 'embed model mismatch' in error, got: %v", err)
	}
}

func TestSolutionValidation_IncompatibleDimensions(t *testing.T) {
	cfg := Config{
		Projects: makeTestProjects(),
		Solutions: map[string]SolutionConfig{
			"mi-suite": {Projects: []string{"pagos", "other-dims"}},
		},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for mismatched dimensions, got nil")
	}
	if !strings.Contains(err.Error(), "embed dimensions mismatch") {
		t.Errorf("expected 'embed dimensions mismatch' in error, got: %v", err)
	}
}

func TestSolutionValidation_UnknownProject(t *testing.T) {
	cfg := Config{
		Projects: makeTestProjects(),
		Solutions: map[string]SolutionConfig{
			"mi-suite": {Projects: []string{"pagos", "ghost-project"}},
		},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for unknown project, got nil")
	}
	if !strings.Contains(err.Error(), "ghost-project") {
		t.Errorf("expected project name in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected 'not found' in error, got: %v", err)
	}
}

func TestSolutionValidation_MinProjects(t *testing.T) {
	cfg := Config{
		Projects: makeTestProjects(),
		Solutions: map[string]SolutionConfig{
			"mi-suite": {Projects: []string{"pagos"}},
		},
	}
	err := Validate(cfg)
	if err == nil {
		t.Fatal("expected error for single project, got nil")
	}
	if !strings.Contains(err.Error(), "at least 2") {
		t.Errorf("expected 'at least 2' in error, got: %v", err)
	}
}

func TestSolutionRoundTrip(t *testing.T) {
	cfg := Config{
		APIEndpoint: "https://api.example.com",
		TenantToken: "tok-abc",
		Projects: map[string]ProjectConfig{
			"pagos": {
				ProjectID:       "proj-001",
				EmbedModel:      "openai/text-embedding-3-large",
				EmbedDimensions: 3072,
			},
			"portal": {
				ProjectID:       "proj-002",
				EmbedModel:      "openai/text-embedding-3-large",
				EmbedDimensions: 3072,
			},
		},
		Solutions: map[string]SolutionConfig{
			"mi-suite": {Projects: []string{"pagos", "portal"}},
		},
	}

	// Marshal
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Unmarshal
	var loaded Config
	if err := yaml.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	solution, ok := loaded.Solutions["mi-suite"]
	if !ok {
		t.Fatal("solution 'mi-suite' not found after round-trip")
	}
	if !slicesEqual(solution.Projects, []string{"pagos", "portal"}) {
		t.Errorf("solution projects = %v, want [pagos portal]", solution.Projects)
	}
}

func TestResolveSolution_Found(t *testing.T) {
	cfg := Config{
		Projects: makeTestProjects(),
		Solutions: map[string]SolutionConfig{
			"mi-suite": {Projects: []string{"pagos", "portal"}},
		},
	}
	projects, err := cfg.ResolveSolution("mi-suite")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !slicesEqual(projects, []string{"pagos", "portal"}) {
		t.Errorf("projects = %v, want [pagos portal]", projects)
	}
}

func TestResolveSolution_NotFound(t *testing.T) {
	cfg := Config{
		Projects:  makeTestProjects(),
		Solutions: map[string]SolutionConfig{},
	}
	_, err := cfg.ResolveSolution("ghost")
	if err == nil {
		t.Fatal("expected error for missing solution, got nil")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("expected solution name in error, got: %v", err)
	}
}
