// Package tui implements the setup wizard TUI for brain-context.
//
// Patterns (following Engram / Gentleman Bubbletea conventions):
// - Screen constants as iota
// - Single Model struct holds ALL state
// - Update() with type-switch on tea.Msg
// - Per-screen key handlers returning (tea.Model, tea.Cmd)
// - Vim keys where applicable
package tui

import (
	"github.com/Gentleman-Programming/brain-context/internal/version"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ─── Screens ─────────────────────────────────────────────────────────────────

type Screen int

const (
	ScreenConnect  Screen = iota // Step 1 — API URL + tenant token
	ScreenEmbedder               // Step 2 — embedder provider + key
	ScreenClients                // Step 3 — select AI clients
	ScreenDone                   // Step 4 — summary + next steps
)

const totalSteps = 4

// ─── Embedder options ─────────────────────────────────────────────────────────

type embedderOption struct {
	label string
	value string // provider prefix
	model string
	hint  string
}

var embedderOptions = []embedderOption{
	{label: "OpenAI", value: "openai", model: "text-embedding-3-large", hint: "Best quality · paid · sk-..."},
	{label: "Gemini", value: "gemini", model: "gemini-embedding-001", hint: "Good quality · free tier available · AIza..."},
	{label: "Ollama (local)", value: "ollama", model: "nomic-embed-text", hint: "Free · private · needs Ollama running"},
}

// ─── Client options ───────────────────────────────────────────────────────────

type clientOption struct {
	id    string
	label string
}

var clientOptions = []clientOption{
	{id: "opencode", label: "OpenCode"},
	{id: "claude", label: "Claude Code"},
	{id: "cursor", label: "Cursor"},
	{id: "gemini", label: "Gemini CLI"},
	{id: "windsurf", label: "Windsurf"},
}

// ─── Messages ─────────────────────────────────────────────────────────────────

type loginDoneMsg struct{ err error }
type setupClientsDoneMsg struct {
	results map[string]error
}
type updateCheckMsg struct {
	result version.CheckResult
}

// ─── Model ───────────────────────────────────────────────────────────────────

type Model struct {
	// layout
	width  int
	height int

	// navigation
	screen Screen

	// update check
	updateStatus  version.CheckStatus
	updateMsg     string
	latestVersion string
	currentVersion string

	// step 1 — connect
	apiInput   textinput.Model
	tokenInput textinput.Model
	focusIdx   int // 0=api, 1=token
	loginErr   string
	logging    bool
	spinner    spinner.Model

	// step 2 — embedder
	embedderIdx int // selected embedder option
	keyInput    textinput.Model
	embedFocus  int // 0=list, 1=key input

	// step 3 — clients
	clientChecked [5]bool
	clientCursor  int

	// step 4 — done
	clientResults map[string]error

	// shared
	brainExe string
	apiURL   string
	token    string
}

// New creates a fresh setup wizard model.
func New(brainExe string, currentVersion string) Model {
	api := textinput.New()
	api.Placeholder = "https://brain.mycompany.com"
	api.Width = 52
	api.Focus()

	tok := textinput.New()
	tok.Placeholder = "brn_tenant_..."
	tok.EchoMode = textinput.EchoPassword
	tok.EchoCharacter = '•'
	tok.Width = 52

	key := textinput.New()
	key.Placeholder = "API key (leave empty for Ollama)"
	key.EchoMode = textinput.EchoPassword
	key.EchoCharacter = '•'
	key.Width = 52

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(colorLavender)

	// Default: all clients checked
	var checked [5]bool
	for i := range checked {
		checked[i] = true
	}

	return Model{
		screen:         ScreenConnect,
		apiInput:       api,
		tokenInput:     tok,
		keyInput:       key,
		spinner:        sp,
		clientChecked:  checked,
		brainExe:       brainExe,
		currentVersion: currentVersion,
	}
}

// Init starts the spinner tick and kicks off the update check in background.
func (m Model) Init() tea.Cmd {
	return tea.Batch(
		textinput.Blink,
		tea.EnterAltScreen,
		checkForUpdate(m.currentVersion),
	)
}

func checkForUpdate(current string) tea.Cmd {
	return func() tea.Msg {
		return updateCheckMsg{result: version.CheckLatest(current)}
	}
}
