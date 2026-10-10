package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// ── runOKFRegister flag parsing ───────────────────────────────────────────────

func TestRunOKFRegister_MissingBundle(t *testing.T) {
	err := runOKFRegister([]string{"--project", "myapp"})
	if err == nil {
		t.Fatal("expected error for missing --bundle, got nil")
	}
	if !strings.Contains(err.Error(), "bundle") {
		t.Errorf("error %q does not mention 'bundle'", err.Error())
	}
}

func TestRunOKFRegister_MissingProject(t *testing.T) {
	err := runOKFRegister([]string{"--bundle", "/some/path"})
	if err == nil {
		t.Fatal("expected error for missing --project, got nil")
	}
	if !strings.Contains(err.Error(), "project") {
		t.Errorf("error %q does not mention 'project'", err.Error())
	}
}

func TestRunOKFRegister_NonExistentBundlePath(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	// Set up a config with a registered project
	writeTestConfig(t, tmpHome, "myapp", "proj-register-test")

	err := runOKFRegister([]string{
		"--project", "myapp",
		"--bundle", "/this/path/does/not/exist",
	})
	if err == nil {
		t.Fatal("expected error for non-existent bundle path, got nil")
	}
	if !strings.Contains(err.Error(), "exist") && !strings.Contains(err.Error(), "not") {
		t.Errorf("error should mention path doesn't exist: %v", err)
	}
}

func TestRunOKFRegister_UnregisteredProject(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	bundleDir := t.TempDir()

	// Config with no projects
	writeEmptyConfig(t, tmpHome)

	err := runOKFRegister([]string{
		"--project", "nonexistent-project",
		"--bundle", bundleDir,
	})
	if err == nil {
		t.Fatal("expected error for unregistered project, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent-project") {
		t.Errorf("error should mention project name: %v", err)
	}
}

func TestRunOKFRegister_ValidCallSucceeds(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	bundleDir := t.TempDir()
	writeTestConfig(t, tmpHome, "myapp", "proj-valid-register")

	// An empty bundle dir is valid — just no concepts to index
	err := runOKFRegister([]string{
		"--project", "myapp",
		"--bundle", bundleDir,
	})
	if err != nil {
		t.Fatalf("runOKFRegister with valid args: %v", err)
	}

	// Config should now have OKF fields written
	cfgPath := filepath.Join(tmpHome, ".brain", "config.yaml")
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "okf_bundle_path") {
		t.Errorf("config missing okf_bundle_path after register:\n%s", data)
	}
	if !strings.Contains(string(data), "okf_bundle_id") {
		t.Errorf("config missing okf_bundle_id after register:\n%s", data)
	}
}

// ── runOKFGenerate ───────────────────────────────────────────────────────────

func TestRunOKFGenerate_MissingProject(t *testing.T) {
	err := runOKFGenerate([]string{})
	if err == nil {
		t.Fatal("expected error for missing --project, got nil")
	}
	if !strings.Contains(err.Error(), "project") {
		t.Errorf("error %q does not mention 'project'", err.Error())
	}
}

func TestRunOKFGenerate_NoBundleRegistered(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	writeTestConfig(t, tmpHome, "myapp", "proj-gen-nobundle")

	err := runOKFGenerate([]string{"--project", "myapp"})
	if err == nil {
		t.Fatal("expected error for project with no OKF bundle, got nil")
	}
	if !strings.Contains(err.Error(), "OKF bundle") {
		t.Errorf("error should mention OKF bundle: %v", err)
	}
}

// ── runOKFList ───────────────────────────────────────────────────────────────

func TestRunOKFList_MissingProject(t *testing.T) {
	err := runOKFList([]string{})
	if err == nil {
		t.Fatal("expected error for missing --project, got nil")
	}
	if !strings.Contains(err.Error(), "project") {
		t.Errorf("error %q does not mention 'project'", err.Error())
	}
}

func TestRunOKFList_NoBundleRegistered(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	t.Setenv("USERPROFILE", tmpHome)

	writeTestConfig(t, tmpHome, "myapp", "proj-list-nobundle")

	err := runOKFList([]string{"--project", "myapp"})
	if err == nil {
		t.Fatal("expected error for project with no OKF bundle, got nil")
	}
}

// ── runOKFValidate ───────────────────────────────────────────────────────────

func TestRunOKFValidate_MissingProject(t *testing.T) {
	err := runOKFValidate([]string{})
	if err == nil {
		t.Fatal("expected error for missing --project, got nil")
	}
	if !strings.Contains(err.Error(), "project") {
		t.Errorf("error %q does not mention 'project'", err.Error())
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

type minimalConfig struct {
	APIEndpoint string                   `yaml:"api_endpoint"`
	TenantToken string                   `yaml:"tenant_token"`
	Projects    map[string]minimalProject `yaml:"projects"`
}

type minimalProject struct {
	ProjectID       string `yaml:"project_id"`
	ProjectToken    string `yaml:"project_token"`
	MCPReadKey      string `yaml:"mcp_read_key"`
	RepoPath        string `yaml:"repo_path"`
	EmbedModel      string `yaml:"embed_model"`
	EmbedDimensions int    `yaml:"embed_dimensions"`
}

// writeTestConfig writes a minimal brain config with one registered project.
func writeTestConfig(t *testing.T, home, projectName, projectID string) {
	t.Helper()
	cfgDir := filepath.Join(home, ".brain")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	cfg := minimalConfig{
		APIEndpoint: "http://localhost:8080",
		TenantToken: "tok-test",
		Projects: map[string]minimalProject{
			projectName: {
				ProjectID:       projectID,
				ProjectToken:    "pt-test",
				MCPReadKey:      "rk-test",
				RepoPath:        t.TempDir(),
				EmbedModel:      "openai/text-embedding-3-large",
				EmbedDimensions: 3072,
			},
		},
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

// writeEmptyConfig writes a brain config with no projects.
func writeEmptyConfig(t *testing.T, home string) {
	t.Helper()
	cfgDir := filepath.Join(home, ".brain")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}
	cfg := minimalConfig{
		APIEndpoint: "http://localhost:8080",
		TenantToken: "tok-test",
		Projects:    map[string]minimalProject{},
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}
