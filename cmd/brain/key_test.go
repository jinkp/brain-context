package main

import (
	"os"
	"path/filepath"
	"testing"

	brainconfig "github.com/Gentleman-Programming/brain-context/internal/config"
	"gopkg.in/yaml.v3"
)

// writeKeyTestConfig writes a brainconfig.Config to a fresh temp HOME and sets
// HOME/USERPROFILE so brainconfig.Load() picks it up.
func writeKeyTestConfig(t *testing.T, cfg brainconfig.Config) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	dir := filepath.Join(home, ".brain")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir .brain: %v", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), data, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return home
}

// ── resolveProjectName ────────────────────────────────────────────────────────

func TestResolveProjectName_LiteralName(t *testing.T) {
	got, err := resolveProjectName("my-project")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "my-project" {
		t.Errorf("got %q, want %q", got, "my-project")
	}
}

func TestResolveProjectName_DotMatchesCWD(t *testing.T) {
	repoDir := t.TempDir()

	writeKeyTestConfig(t, brainconfig.Config{
		APIEndpoint: "http://localhost:8080",
		TenantToken: "tok",
		Projects: map[string]brainconfig.ProjectConfig{
			"my-app": {
				ProjectID:       "proj-uuid-001",
				RepoPath:        repoDir,
				EmbedModel:      "openai/text-embedding-3-large",
				EmbedDimensions: 3072,
			},
		},
	})

	// Change cwd to the repo dir so "." resolves to "my-app"
	orig, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(orig) })
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	got, err := resolveProjectName(".")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "my-app" {
		t.Errorf("got %q, want %q", got, "my-app")
	}
}

func TestResolveProjectName_DotNoMatch(t *testing.T) {
	otherDir := t.TempDir()

	writeKeyTestConfig(t, brainconfig.Config{
		APIEndpoint: "http://localhost:8080",
		TenantToken: "tok",
		Projects: map[string]brainconfig.ProjectConfig{
			"other-app": {
				ProjectID: "proj-uuid-002",
				RepoPath:  t.TempDir(), // different dir
			},
		},
	})

	orig, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(orig) })
	if err := os.Chdir(otherDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	_, err := resolveProjectName(".")
	if err == nil {
		t.Fatal("expected error for unregistered directory, got nil")
	}
}

func TestResolveProjectName_DotEmptyConfig(t *testing.T) {
	writeKeyTestConfig(t, brainconfig.Config{
		APIEndpoint: "http://localhost:8080",
		Projects:    map[string]brainconfig.ProjectConfig{},
	})

	_, err := resolveProjectName(".")
	if err == nil {
		t.Fatal("expected error when no projects registered, got nil")
	}
}

func TestResolveProjectName_DotCaseInsensitive(t *testing.T) {
	repoDir := t.TempDir()

	writeKeyTestConfig(t, brainconfig.Config{
		APIEndpoint: "http://localhost:8080",
		TenantToken: "tok",
		Projects: map[string]brainconfig.ProjectConfig{
			"ci-app": {
				ProjectID:       "proj-uuid-ci",
				RepoPath:        repoDir,
				EmbedModel:      "openai/text-embedding-3-large",
				EmbedDimensions: 3072,
			},
		},
	})

	orig, _ := os.Getwd()
	t.Cleanup(func() { os.Chdir(orig) })
	if err := os.Chdir(repoDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	got, err := resolveProjectName(".")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "ci-app" {
		t.Errorf("got %q, want %q", got, "ci-app")
	}
}
