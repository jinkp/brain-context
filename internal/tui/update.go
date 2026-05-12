package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

var httpClient = &http.Client{Timeout: 15 * time.Second}

// ─── Update ───────────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch m.screen {
		case ScreenConnect:
			return m.updateConnect(msg)
		case ScreenEmbedder:
			return m.updateEmbedder(msg)
		case ScreenClients:
			return m.updateClients(msg)
		case ScreenDone:
			return m.updateDone(msg)
		}

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case loginDoneMsg: //nolint:govet
		m.logging = false
		if msg.err != nil {
			m.loginErr = msg.err.Error()
			return m, nil
		}
		m.screen = ScreenEmbedder
		m.keyInput.Focus()
		return m, textinput.Blink

	case setupClientsDoneMsg:
		m.clientResults = msg.results
		m.screen = ScreenDone
		return m, nil
	}

	// Forward input updates to active inputs
	return m.updateInputs(msg)
}

// ─── Per-screen key handlers ──────────────────────────────────────────────────

func (m Model) updateConnect(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "esc":
		return m, tea.Quit

	case "tab", "shift+tab", "down", "up":
		if msg.String() == "tab" || msg.String() == "down" {
			m.focusIdx = (m.focusIdx + 1) % 2
		} else {
			m.focusIdx = (m.focusIdx + 1) % 2
		}
		if m.focusIdx == 0 {
			m.apiInput.Focus()
			m.tokenInput.Blur()
		} else {
			m.tokenInput.Focus()
			m.apiInput.Blur()
		}
		return m, textinput.Blink

	case "enter":
		apiURL := strings.TrimRight(strings.TrimSpace(m.apiInput.Value()), "/")
		token := strings.TrimSpace(m.tokenInput.Value())

		if apiURL == "" {
			m.loginErr = "API URL is required"
			return m, nil
		}
		if token == "" {
			m.loginErr = "Tenant token is required"
			return m, nil
		}

		m.loginErr = ""
		m.logging = true
		m.apiURL = apiURL
		m.token = token
		return m, tea.Batch(m.spinner.Tick, doLogin(apiURL, token))
	}

	return m.updateInputs(msg)
}

func (m Model) updateEmbedder(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.screen = ScreenConnect
		m.apiInput.Focus()
		m.tokenInput.Blur()
		m.embedFocus = 0
		return m, textinput.Blink

	case "up", "k":
		if m.embedFocus == 0 {
			m.embedderIdx = max(0, m.embedderIdx-1)
		}
	case "down", "j":
		if m.embedFocus == 0 {
			m.embedderIdx = min(len(embedderOptions)-1, m.embedderIdx+1)
		}

	case "tab":
		if m.embedFocus == 0 {
			m.embedFocus = 1
			m.keyInput.Focus()
		} else {
			m.embedFocus = 0
			m.keyInput.Blur()
		}
		return m, textinput.Blink

	case "enter":
		// Ollama doesn't need an API key
		opt := embedderOptions[m.embedderIdx]
		if opt.value != "ollama" && strings.TrimSpace(m.keyInput.Value()) == "" {
			m.embedFocus = 1
			m.keyInput.Focus()
			return m, textinput.Blink
		}
		m.screen = ScreenClients
		return m, nil
	}

	return m.updateInputs(msg)
}

func (m Model) updateClients(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.screen = ScreenEmbedder
		return m, nil

	case "up", "k":
		m.clientCursor = max(0, m.clientCursor-1)
	case "down", "j":
		m.clientCursor = min(len(clientOptions)-1, m.clientCursor+1)

	case " ":
		m.clientChecked[m.clientCursor] = !m.clientChecked[m.clientCursor]

	case "a":
		// toggle all
		all := true
		for _, c := range m.clientChecked {
			if !c {
				all = false
				break
			}
		}
		for i := range m.clientChecked {
			m.clientChecked[i] = !all
		}

	case "enter":
		selected := make([]string, 0)
		for i, opt := range clientOptions {
			if m.clientChecked[i] {
				selected = append(selected, opt.id)
			}
		}
		if len(selected) == 0 {
			m.screen = ScreenDone
			m.clientResults = map[string]error{}
			return m, nil
		}
		return m, doSetupClients(m.brainExe, selected)
	}

	return m, nil
}

func (m Model) updateDone(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c", "esc", "enter":
		return m, tea.Quit
	}
	return m, nil
}

// ─── Input forwarding ─────────────────────────────────────────────────────────

func (m Model) updateInputs(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	var cmd tea.Cmd

	switch m.screen {
	case ScreenConnect:
		if m.focusIdx == 0 {
			m.apiInput, cmd = m.apiInput.Update(msg)
		} else {
			m.tokenInput, cmd = m.tokenInput.Update(msg)
		}
		cmds = append(cmds, cmd)

	case ScreenEmbedder:
		if m.embedFocus == 1 {
			m.keyInput, cmd = m.keyInput.Update(msg)
			cmds = append(cmds, cmd)
		}

	default:
		// spinner tick forwarding
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
	}

	return m, tea.Batch(cmds...)
}

// ─── Commands ─────────────────────────────────────────────────────────────────

type authLoginReq struct {
	APIKey string `json:"api_key"`
}

type apiErrResp struct {
	Error string `json:"error"`
}

func doLogin(apiURL, token string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		payload, _ := json.Marshal(authLoginReq{APIKey: token})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost,
			apiURL+"/api/auth/login", bytes.NewReader(payload))
		if err != nil {
			return loginDoneMsg{err: err}
		}
		req.Header.Set("Content-Type", "application/json")

		resp, err := httpClient.Do(req)
		if err != nil {
			return loginDoneMsg{err: fmt.Errorf("cannot reach API: %w", err)}
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode >= 400 {
			var apiErr apiErrResp
			if json.Unmarshal(body, &apiErr) == nil && apiErr.Error != "" {
				return loginDoneMsg{err: errors.New(apiErr.Error)}
			}
			return loginDoneMsg{err: fmt.Errorf("login failed (%s)", resp.Status)}
		}
		return loginDoneMsg{err: nil}
	}
}

func doSetupClients(brainExe string, clients []string) tea.Cmd {
	return func() tea.Msg {
		home := userHome()
		results := make(map[string]error, len(clients))
		for _, c := range clients {
			results[c] = setupClient(c, home, brainExe)
		}
		return setupClientsDoneMsg{results: results}
	}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
