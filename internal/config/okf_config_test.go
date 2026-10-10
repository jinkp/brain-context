package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ── OKFThreshold validation ───────────────────────────────────────────────────

func TestOKFThreshold_Validation(t *testing.T) {
	baseProject := ProjectConfig{
		ProjectID:       "proj-001",
		EmbedModel:      "openai/text-embedding-3-large",
		EmbedDimensions: 3072,
	}

	cases := []struct {
		name      string
		threshold float64
		wantErr   bool
		errSub    string
	}{
		{
			name:      "threshold 0.5 — valid",
			threshold: 0.5,
			wantErr:   false,
		},
		{
			name:      "threshold 1.0 — valid (upper bound)",
			threshold: 1.0,
			wantErr:   false,
		},
		{
			name:      "threshold 0.0 — valid (zero means use default)",
			threshold: 0.0,
			wantErr:   false,
		},
		{
			name:      "threshold 1.5 — invalid (above 1.0)",
			threshold: 1.5,
			wantErr:   true,
			errSub:    "okf_threshold",
		},
		{
			name:      "threshold -0.1 — invalid (below 0.0)",
			threshold: -0.1,
			wantErr:   true,
			errSub:    "okf_threshold",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := baseProject
			p.OKFThreshold = tc.threshold
			cfg := Config{
				Projects: map[string]ProjectConfig{
					"myapp": p,
				},
			}
			err := Validate(cfg)
			if tc.wantErr && err == nil {
				t.Errorf("Validate() expected error for threshold %.2f, got nil", tc.threshold)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("Validate() unexpected error for threshold %.2f: %v", tc.threshold, err)
			}
			if tc.wantErr && err != nil && tc.errSub != "" {
				if !strings.Contains(err.Error(), tc.errSub) {
					t.Errorf("error %q does not contain %q", err.Error(), tc.errSub)
				}
			}
		})
	}
}

// ── OKF field whitespace normalization ────────────────────────────────────────

func TestOKFFields_NormalizeTrimWhitespace(t *testing.T) {
	const yamlWithSpaces = `api_endpoint: https://api.brain-context.dev
tenant_token: tok-abc123
projects:
  myapp:
    project_id: proj-001
    embed_model: openai/text-embedding-3-large
    embed_dimensions: 3072
    okf_bundle_path: "  /home/user/okf-bundles/myapp  "
    okf_visibility: "  local  "
    okf_bundle_id: "  uuid-abc123  "
`

	var cfg Config
	if err := yaml.Unmarshal([]byte(yamlWithSpaces), &cfg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	normalize(&cfg)

	proj := cfg.Projects["myapp"]
	if proj.OKFBundlePath != "/home/user/okf-bundles/myapp" {
		t.Errorf("OKFBundlePath = %q, want trimmed %q", proj.OKFBundlePath, "/home/user/okf-bundles/myapp")
	}
	if proj.OKFVisibility != "local" {
		t.Errorf("OKFVisibility = %q, want %q", proj.OKFVisibility, "local")
	}
	if proj.OKFBundleID != "uuid-abc123" {
		t.Errorf("OKFBundleID = %q, want %q", proj.OKFBundleID, "uuid-abc123")
	}
}

// ── OKFThreshold not normalized (float, no trim needed) ──────────────────────

func TestOKFThreshold_NotAffectedByNormalize(t *testing.T) {
	cfg := Config{
		Projects: map[string]ProjectConfig{
			"myapp": {
				ProjectID:       "proj-001",
				EmbedModel:      "openai/text-embedding-3-large",
				EmbedDimensions: 3072,
				OKFThreshold:    0.75,
			},
		},
	}
	normalize(&cfg)
	if cfg.Projects["myapp"].OKFThreshold != 0.75 {
		t.Errorf("OKFThreshold changed after normalize: %f", cfg.Projects["myapp"].OKFThreshold)
	}
}

// ── Backward compatibility: existing configs load without OKF fields ──────────

func TestOKFFields_BackwardCompatibility(t *testing.T) {
	const oldYAML = `api_endpoint: https://api.brain-context.dev
tenant_token: tok-abc123
projects:
  myapp:
    project_id: proj-001
    project_token: pt-xyz
    mcp_read_key: rk-111
    repo_path: /home/user/myapp
    embed_model: openai/text-embedding-3-large
    embed_dimensions: 3072
`

	var cfg Config
	if err := yaml.Unmarshal([]byte(oldYAML), &cfg); err != nil {
		t.Fatalf("unmarshal old config: %v", err)
	}
	normalize(&cfg)
	if err := Validate(cfg); err != nil {
		t.Fatalf("validate old config (no OKF fields): %v", err)
	}

	proj := cfg.Projects["myapp"]
	if proj.OKFBundlePath != "" {
		t.Errorf("OKFBundlePath = %q, want empty (backward compat)", proj.OKFBundlePath)
	}
	if proj.OKFVisibility != "" {
		t.Errorf("OKFVisibility = %q, want empty", proj.OKFVisibility)
	}
	if proj.OKFBundleID != "" {
		t.Errorf("OKFBundleID = %q, want empty", proj.OKFBundleID)
	}
	if proj.OKFThreshold != 0 {
		t.Errorf("OKFThreshold = %f, want 0", proj.OKFThreshold)
	}
}

// ── OKF fields omitted from YAML when not set ─────────────────────────────────

func TestOKFFields_OmitEmptyInYAML(t *testing.T) {
	cfg := Config{
		APIEndpoint: "https://api.brain-context.dev",
		TenantToken: "tok-abc",
		Projects: map[string]ProjectConfig{
			"myapp": {
				ProjectID:       "proj-001",
				EmbedModel:      "openai/text-embedding-3-large",
				EmbedDimensions: 3072,
			},
		},
	}

	out, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	output := string(out)

	for _, key := range []string{"okf_bundle_path", "okf_visibility", "okf_bundle_id", "okf_threshold"} {
		if strings.Contains(output, key) {
			t.Errorf("YAML output contains %q but should be omitted (omitempty): %s", key, output)
		}
	}
}

// ── OKF fields present in YAML when set ──────────────────────────────────────

func TestOKFFields_PresentInYAMLWhenSet(t *testing.T) {
	cfg := Config{
		APIEndpoint: "https://api.brain-context.dev",
		TenantToken: "tok-abc",
		Projects: map[string]ProjectConfig{
			"myapp": {
				ProjectID:       "proj-001",
				EmbedModel:      "openai/text-embedding-3-large",
				EmbedDimensions: 3072,
				OKFBundlePath:   "/home/user/bundle",
				OKFVisibility:   "local",
				OKFBundleID:     "uuid-xyz",
				OKFThreshold:    0.8,
			},
		},
	}

	out, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	output := string(out)

	for _, want := range []string{"okf_bundle_path", "okf_visibility", "okf_bundle_id", "okf_threshold"} {
		if !strings.Contains(output, want) {
			t.Errorf("YAML output missing %q:\n%s", want, output)
		}
	}
}
