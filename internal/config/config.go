package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

var allowedDimensions = map[int]struct{}{
	768:  {},
	1024: {},
	3072: {},
}

type Config struct {
	APIEndpoint string                   `yaml:"api_endpoint"`
	TenantToken string                   `yaml:"tenant_token"`
	Projects    map[string]ProjectConfig `yaml:"projects"`
}

type ProjectConfig struct {
	ProjectID       string `yaml:"project_id"`
	ProjectToken    string `yaml:"project_token"`
	MCPReadKey      string `yaml:"mcp_read_key"`
	RepoPath        string `yaml:"repo_path"`
	EmbedModel      string `yaml:"embed_model"`
	EmbedAPIKey     string `yaml:"embed_api_key"`
	EmbedDimensions int    `yaml:"embed_dimensions"`
}

func Path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(home, ".brain", "config.yaml"), nil
}

func Load() (Config, error) {
	path, err := Path()
	if err != nil {
		return Config{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Config{}, fmt.Errorf("create config directory: %w", err)
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		cfg := Config{Projects: map[string]ProjectConfig{}}
		if err := Save(cfg); err != nil {
			return Config{}, err
		}
		return cfg, nil
	} else if err != nil {
		return Config{}, fmt.Errorf("stat config file: %w", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config file: %w", err)
	}

	var cfg Config
	if len(data) > 0 {
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return Config{}, fmt.Errorf("parse config file: %w", err)
		}
	}
	normalize(&cfg)
	if err := Validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func Save(cfg Config) error {
	path, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	normalize(&cfg)
	if err := Validate(cfg); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config file: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config file: %w", err)
	}
	return nil
}

func Validate(cfg Config) error {
	for name, project := range cfg.Projects {
		name = strings.TrimSpace(name)
		if name == "" {
			return fmt.Errorf("project name cannot be empty")
		}
		if _, ok := allowedDimensions[project.EmbedDimensions]; !ok {
			return fmt.Errorf("project %q embed_dimensions must be one of 768, 1024, or 3072", name)
		}
	}
	return nil
}

func IsAllowedDimension(dim int) bool {
	_, ok := allowedDimensions[dim]
	return ok
}

func normalize(cfg *Config) {
	if cfg.Projects == nil {
		cfg.Projects = map[string]ProjectConfig{}
	}
	cfg.APIEndpoint = strings.TrimRight(strings.TrimSpace(cfg.APIEndpoint), "/")
	cfg.TenantToken = strings.TrimSpace(cfg.TenantToken)
	projects := make(map[string]ProjectConfig, len(cfg.Projects))
	for name, project := range cfg.Projects {
		trimmedName := strings.TrimSpace(name)
		if trimmedName == "" {
			continue
		}
		project.ProjectID = strings.TrimSpace(project.ProjectID)
		project.ProjectToken = strings.TrimSpace(project.ProjectToken)
		project.MCPReadKey = strings.TrimSpace(project.MCPReadKey)
		project.RepoPath = strings.TrimSpace(project.RepoPath)
		project.EmbedModel = strings.TrimSpace(project.EmbedModel)
		project.EmbedAPIKey = strings.TrimSpace(project.EmbedAPIKey)
		projects[trimmedName] = project
	}
	cfg.Projects = projects
}
