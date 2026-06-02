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

type SolutionConfig struct {
	Projects       []string `yaml:"projects"`                   // project names (keys in Config.Projects)
	SmartThreshold float64  `yaml:"smart_threshold,omitempty"` // 0 → runtime default 0.50; valid range [0.0, 1.0]
}

type Config struct {
	APIEndpoint string                     `yaml:"api_endpoint"`
	TenantToken string                     `yaml:"tenant_token"`
	Exclude     []string                   `yaml:"exclude,omitempty"` // global dirs to exclude from all projects
	Include     []string                   `yaml:"include,omitempty"` // global extra extensions for all projects
	Projects    map[string]ProjectConfig   `yaml:"projects"`
	Solutions   map[string]SolutionConfig  `yaml:"solutions,omitempty"`
}

type ProjectConfig struct {
	ProjectID       string   `yaml:"project_id"`
	ProjectToken    string   `yaml:"project_token"`
	MCPReadKey      string   `yaml:"mcp_read_key"`
	RepoPath        string   `yaml:"repo_path"`
	EmbedModel      string   `yaml:"embed_model"`
	EmbedAPIKey     string   `yaml:"embed_api_key"`      // plaintext — only set by admin locally
	EmbedAPIKeyEnc  string   `yaml:"embed_api_key_enc"`  // AES-256-GCM encrypted — set via brain join
	EmbedDimensions int      `yaml:"embed_dimensions"`
	Exclude         []string `yaml:"exclude,omitempty"` // per-project dirs to exclude
	Include         []string `yaml:"include,omitempty"` // per-project extra extensions to include
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
		cfg := Config{
			Projects:  map[string]ProjectConfig{},
			Solutions: map[string]SolutionConfig{},
		}
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

	for solutionName, solution := range cfg.Solutions {
		if len(solution.Projects) < 2 {
			return fmt.Errorf("solution %q must have at least 2 projects", solutionName)
		}
		if solution.SmartThreshold != 0 && (solution.SmartThreshold < 0.0 || solution.SmartThreshold > 1.0) {
			return fmt.Errorf("solution %q: smart_threshold must be in [0.0, 1.0]", solutionName)
		}
		var referenceModel string
		var referenceDims int
		var referenceProjectName string
		for _, projectName := range solution.Projects {
			proj, ok := cfg.Projects[projectName]
			if !ok {
				return fmt.Errorf("solution %q: project %q not found", solutionName, projectName)
			}
			if referenceModel == "" {
				referenceModel = proj.EmbedModel
				referenceDims = proj.EmbedDimensions
				referenceProjectName = projectName
				continue
			}
			if proj.EmbedModel != referenceModel {
				return fmt.Errorf("solution %q: embed model mismatch (project %q uses %s but project %q uses %s)",
					solutionName, projectName, proj.EmbedModel, referenceProjectName, referenceModel)
			}
			if proj.EmbedDimensions != referenceDims {
				return fmt.Errorf("solution %q: embed dimensions mismatch (project %q uses %d but project %q uses %d)",
					solutionName, projectName, proj.EmbedDimensions, referenceProjectName, referenceDims)
			}
		}
	}

	return nil
}

func IsAllowedDimension(dim int) bool {
	_, ok := allowedDimensions[dim]
	return ok
}

// ResolveSolution returns the project names for a solution, or an error if the solution is not found.
func (cfg Config) ResolveSolution(name string) ([]string, error) {
	solution, ok := cfg.Solutions[name]
	if !ok {
		return nil, fmt.Errorf("solution %q not found", name)
	}
	return solution.Projects, nil
}

func normalize(cfg *Config) {
	if cfg.Projects == nil {
		cfg.Projects = map[string]ProjectConfig{}
	}
	if cfg.Solutions == nil {
		cfg.Solutions = map[string]SolutionConfig{}
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
