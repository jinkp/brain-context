package mcp

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	brainconfig "github.com/Gentleman-Programming/brain-context/internal/config"
	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	serverName    = "brain-context"
	serverVersion = "0.1.0"
)

type Server struct {
	apiEndpoint      string
	defaultProjectID string
	projectsByID     map[string]brainconfig.ProjectConfig
	projectsByName   map[string]string // name → project_id
	httpClient       *http.Client
}

func New(defaultProjectName string) (*Server, error) {
	cfg, err := brainconfig.Load()
	if err != nil {
		return nil, errLoginRequired
	}
	if strings.TrimSpace(cfg.APIEndpoint) == "" {
		return nil, errLoginRequired
	}

	projectsByID := make(map[string]brainconfig.ProjectConfig, len(cfg.Projects))
	projectsByName := make(map[string]string, len(cfg.Projects))
	for name, project := range cfg.Projects {
		if id := strings.TrimSpace(project.ProjectID); id != "" {
			projectsByID[id] = project
			projectsByName[strings.ToLower(strings.TrimSpace(name))] = id
		}
	}

	srv := &Server{
		apiEndpoint:    strings.TrimRight(strings.TrimSpace(cfg.APIEndpoint), "/"),
		projectsByID:   projectsByID,
		projectsByName: projectsByName,
		httpClient:     &http.Client{Timeout: 30 * time.Second},
	}
	if strings.TrimSpace(defaultProjectName) != "" {
		project, ok := cfg.Projects[strings.TrimSpace(defaultProjectName)]
		if !ok {
			return nil, projectNotRegisteredError{name: strings.TrimSpace(defaultProjectName)}
		}
		srv.defaultProjectID = strings.TrimSpace(project.ProjectID)
	}

	return srv, nil
}

func (s *Server) Start() error {
	mcpServer := server.NewMCPServer(
		serverName,
		serverVersion,
		server.WithToolCapabilities(true),
		server.WithRecovery(),
	)

	s.registerTools(mcpServer)

	if err := server.ServeStdio(mcpServer); err != nil {
		fmt.Fprintf(os.Stderr, "MCP error: %v\n", err)
		return err
	}
	return nil
}

func (s *Server) registerTools(mcpServer *server.MCPServer) {
	mcpServer.AddTool(searchProjectContextTool(), s.handleSearchProjectContext)
	mcpServer.AddTool(getFileSummaryTool(), s.handleGetFileSummary)
	mcpServer.AddTool(getRelatedFilesTool(), s.handleGetRelatedFiles)
	mcpServer.AddTool(explainFlowTool(), s.handleExplainFlow)
	mcpServer.AddTool(findImpactTool(), s.handleFindImpact)
}

func textResult(text string) *mcpgo.CallToolResult {
	return mcpgo.NewToolResultText(strings.TrimSpace(text))
}
