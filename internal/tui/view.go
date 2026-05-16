package tui

import (
	"fmt"
	"os"
	"strings"

	"github.com/Gentleman-Programming/brain-context/internal/version"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
)

const logo = `
  ██████╗ ██████╗  █████╗ ██╗███╗   ██╗
  ██╔══██╗██╔══██╗██╔══██╗██║████╗  ██║
  ██████╔╝██████╔╝███████║██║██╔██╗ ██║
  ██╔══██╗██╔══██╗██╔══██║██║██║╚██╗██║
  ██████╔╝██║  ██║██║  ██║██║██║ ╚████║
  ╚═════╝ ╚═╝  ╚═╝╚═╝  ╚═╝╚═╝╚═╝  ╚═══╝  context`

// ─── View ─────────────────────────────────────────────────────────────────────

func (m Model) View() string {
	var b strings.Builder

	b.WriteString(logoStyle.Render(logo))
	b.WriteString("\n")

	switch m.screen {
	case ScreenMenu:
		b.WriteString(m.viewMenu())
	case ScreenConnect, ScreenEmbedder:
		// Wizard steps get the step bar
		b.WriteString(renderStepBar(m.screen))
		b.WriteString("\n\n")
		if m.screen == ScreenConnect {
			b.WriteString(m.viewConnect())
		} else {
			b.WriteString(m.viewEmbedder())
		}
	case ScreenClients:
		if m.doneSource != ScreenClients {
			// Inside wizard flow
			b.WriteString(renderStepBar(m.screen))
			b.WriteString("\n\n")
		}
		b.WriteString(m.viewClients())
	case ScreenUpdating:
		b.WriteString(m.viewUpdating())
	case ScreenDone:
		if m.doneSource != ScreenClients {
			b.WriteString(renderStepBar(m.screen))
			b.WriteString("\n\n")
		}
		b.WriteString(m.viewDone())
	}

	return appStyle.Render(b.String())
}

// ─── Step bar ─────────────────────────────────────────────────────────────────

func renderStepBar(screen Screen) string {
	steps := []string{"Connect", "Embedder", "Clients", "Done"}
	// Map screen to wizard step index (ScreenConnect=0, ScreenEmbedder=1, etc.)
	stepMap := map[Screen]int{
		ScreenConnect:  0,
		ScreenEmbedder: 1,
		ScreenClients:  2,
		ScreenDone:     3,
	}
	current := stepMap[screen]
	parts := make([]string, len(steps))
	for i, s := range steps {
		if i == current {
			parts[i] = stepActiveStyle.Render(fmt.Sprintf("● %s", s))
		} else if i < current {
			parts[i] = checkedStyle.Render(fmt.Sprintf("✓ %s", s))
		} else {
			parts[i] = stepStyle.Render(fmt.Sprintf("○ %s", s))
		}
	}
	return strings.Join(parts, dimStyle.Render("  ──  "))
}

// ─── Main Menu ────────────────────────────────────────────────────────────────

func (m Model) viewMenu() string {
	var b strings.Builder

	b.WriteString(headerStyle.Render("What would you like to do?"))
	b.WriteString("\n\n")

	for i, opt := range menuOptions {
		cursor := "  "
		if i == m.menuCursor {
			cursor = selectedStyle.Render("> ")
		}

		label := normalStyle.Render(opt.label)
		if i == m.menuCursor {
			label = selectedStyle.Render(opt.label)
		}

		hint := dimStyle.Render(opt.hint)
		b.WriteString(fmt.Sprintf("%s%s\n", cursor, label))
		b.WriteString(fmt.Sprintf("    %s\n\n", hint))
	}

	// Show update badge if available
	if m.updateStatus == version.StatusUpdateAvailable {
		badge := fmt.Sprintf("  v%s available", m.latestVersion)
		b.WriteString(lipgloss.NewStyle().
			Foreground(colorYellow).
			Bold(true).
			Render(badge))
		b.WriteString("\n\n")
	}

	b.WriteString(helpStyle.Render("j/k navigate  •  Enter select  •  q quit"))

	return b.String()
}

// ─── Updating ─────────────────────────────────────────────────────────────────

func (m Model) viewUpdating() string {
	var b strings.Builder

	b.WriteString(headerStyle.Render("Self-Update"))
	b.WriteString("\n\n")

	if m.updating {
		b.WriteString(m.spinner.View())
		b.WriteString(dimStyle.Render("  Downloading latest version..."))
	} else if m.updateErr != "" {
		b.WriteString(errorStyle.Render("  Update failed: " + m.updateErr))
		b.WriteString("\n\n")
		b.WriteString(helpStyle.Render("Enter back to menu  •  Esc back"))
	} else {
		b.WriteString(successStyle.Render("  Updated successfully!"))
		b.WriteString("\n\n")
		b.WriteString(dimStyle.Render("  Restart brain to use the new version."))
		b.WriteString("\n\n")
		b.WriteString(helpStyle.Render("Enter to exit"))
	}

	return b.String()
}

// ─── Step 1: Connect ──────────────────────────────────────────────────────────

func (m Model) viewConnect() string {
	var b strings.Builder

	b.WriteString(headerStyle.Render("Step 1 — Connect to your team API"))
	b.WriteString("\n\n")

	// API URL
	b.WriteString(labelStyle.Render("API URL"))
	b.WriteString("\n")
	if m.focusIdx == 0 {
		b.WriteString(focusedInputStyle.Render(m.apiInput.View()))
	} else {
		b.WriteString(blurredInputStyle.Render(m.apiInput.View()))
	}
	b.WriteString("\n\n")

	// Token
	b.WriteString(labelStyle.Render("Tenant Token"))
	b.WriteString("\n")
	if m.focusIdx == 1 {
		b.WriteString(focusedInputStyle.Render(m.tokenInput.View()))
	} else {
		b.WriteString(blurredInputStyle.Render(m.tokenInput.View()))
	}
	b.WriteString("\n")

	// Error or spinner
	if m.logging {
		b.WriteString("\n")
		b.WriteString(m.spinner.View())
		b.WriteString(dimStyle.Render("  Verifying token..."))
	} else if m.loginErr != "" {
		b.WriteString("\n")
		b.WriteString(errorStyle.Render("  ✗ " + m.loginErr))
	}

	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("Tab next field  •  Enter continue  •  Esc quit"))

	return b.String()
}

// ─── Step 2: Embedder ─────────────────────────────────────────────────────────

func (m Model) viewEmbedder() string {
	var b strings.Builder

	b.WriteString(headerStyle.Render("Step 2 — Choose your embedding provider"))
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("The embedder converts your code into searchable vectors."))
	b.WriteString("\n\n")

	for i, opt := range embedderOptions {
		cursor := "  "
		if i == m.embedderIdx && m.embedFocus == 0 {
			cursor = selectedStyle.Render("▶ ")
		} else if i == m.embedderIdx {
			cursor = dimStyle.Render("▶ ")
		}

		label := normalStyle.Render(opt.label)
		if i == m.embedderIdx {
			label = selectedStyle.Render(opt.label)
		}
		hint := dimStyle.Render(opt.hint)
		b.WriteString(fmt.Sprintf("%s%s  %s\n", cursor, label, hint))
	}

	b.WriteString("\n")
	b.WriteString(labelStyle.Render("API Key"))
	b.WriteString("\n")
	if m.embedFocus == 1 {
		b.WriteString(focusedInputStyle.Render(m.keyInput.View()))
	} else {
		b.WriteString(blurredInputStyle.Render(m.keyInput.View()))
	}

	b.WriteString("\n\n")
	b.WriteString(helpStyle.Render("↑↓ select provider  •  Tab focus key field  •  Enter continue  •  Esc back"))

	return b.String()
}

// ─── Step 3: Clients ──────────────────────────────────────────────────────────

func (m Model) viewClients() string {
	var b strings.Builder

	b.WriteString(headerStyle.Render("Step 3 — Configure your AI clients"))
	b.WriteString("\n\n")
	b.WriteString(dimStyle.Render("brain-context MCP tools will be injected into each selected client."))
	b.WriteString("\n\n")

	for i, opt := range clientOptions {
		cursor := "  "
		if i == m.clientCursor {
			cursor = selectedStyle.Render("▶ ")
		}

		check := "[ ]"
		if m.clientChecked[i] {
			check = checkedStyle.Render("[✓]")
		} else {
			check = dimStyle.Render("[ ]")
		}

		label := normalStyle.Render(opt.label)
		if i == m.clientCursor {
			label = selectedStyle.Render(opt.label)
		}

		b.WriteString(fmt.Sprintf("%s%s %s\n", cursor, check, label))
	}

	b.WriteString("\n")
	b.WriteString(helpStyle.Render("↑↓ navigate  •  Space toggle  •  A toggle all  •  Enter continue  •  Esc back"))

	return b.String()
}

// ─── Step 4: Done ─────────────────────────────────────────────────────────────

func (m Model) viewDone() string {
	var b strings.Builder

	b.WriteString(headerStyle.Render("Step 4 — All done!"))
	b.WriteString("\n\n")

	// Client setup results
	if len(m.clientResults) > 0 {
		for _, opt := range clientOptions {
			err, ok := m.clientResults[opt.id]
			if !ok {
				continue
			}
			if err == nil {
				b.WriteString(successStyle.Render("  ✅ " + opt.label + " configured"))
			} else {
				b.WriteString(errorStyle.Render("  ✗ " + opt.label + ": " + err.Error()))
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	// Embedder info
	opt := embedderOptions[m.embedderIdx]
	b.WriteString(panelStyle.Render(
		labelStyle.Render("Embedder: ") + selectedStyle.Render(opt.label) + "\n" +
			labelStyle.Render("Model:    ") + normalStyle.Render(opt.model),
	))
	b.WriteString("\n\n")

	// Next steps
	b.WriteString(selectedStyle.Render("Next steps:"))
	b.WriteString("\n\n")

	b.WriteString(dimStyle.Render("  Register your project:"))
	b.WriteString("\n")
	registerCmd := fmt.Sprintf(
		"  brain register --project <name> --repo ./<repo> \\\n    --embedder %s --model %s --api-key <key>",
		opt.value, opt.model,
	)
	b.WriteString(codeStyle.Render(registerCmd))
	b.WriteString("\n\n")

	b.WriteString(dimStyle.Render("  Index your project:"))
	b.WriteString("\n")
	b.WriteString(codeStyle.Render("  brain index --project <name>"))
	b.WriteString("\n\n")

	b.WriteString(dimStyle.Render("  Restart your IDE — the MCP tools will appear automatically."))
	b.WriteString("\n\n")

	b.WriteString(helpStyle.Render("Enter or Q to exit"))

	return b.String()
}

// ─── helpers ─────────────────────────────────────────────────────────────────

func userHome() string {
	h, _ := os.UserHomeDir()
	return h
}

// Expose spinner type so update.go can use it without an import cycle.
type spinnerTickMsg = spinner.TickMsg

// Expose textinput.Blink so update.go can use it.
var textinputBlink = textinput.Blink

// lipgloss width helper.
var _ = lipgloss.Width
