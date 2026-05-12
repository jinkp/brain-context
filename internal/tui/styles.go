package tui

import "github.com/charmbracelet/lipgloss"

// ─── Colors (Catppuccin Mocha — same palette as Engram) ──────────────────────

var (
	colorBase    = lipgloss.Color("#1e1e2e")
	colorSurface = lipgloss.Color("#313244")
	colorOverlay = lipgloss.Color("#6c7086")
	colorText    = lipgloss.Color("#cdd6f4")
	colorSubtext = lipgloss.Color("#a6adc8")
	colorLavender = lipgloss.Color("#b4befe")
	colorBlue    = lipgloss.Color("#89b4fa")
	colorGreen   = lipgloss.Color("#a6e3a1")
	colorYellow  = lipgloss.Color("#f9e2af")
	colorPeach   = lipgloss.Color("#fab387")
	colorRed     = lipgloss.Color("#f38ba8")
	colorMauve   = lipgloss.Color("#cba6f7")
	colorTeal    = lipgloss.Color("#94e2d5")
)

// ─── Layout ───────────────────────────────────────────────────────────────────

var (
	appStyle = lipgloss.NewStyle().
		Padding(1, 2)

	logoStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(colorMauve)

	headerStyle = lipgloss.NewStyle().
		Bold(true).
		Foreground(colorLavender).
		BorderStyle(lipgloss.NormalBorder()).
		BorderBottom(true).
		BorderForeground(colorOverlay).
		PaddingBottom(1).
		MarginBottom(1)

	stepStyle = lipgloss.NewStyle().
		Foreground(colorSubtext)

	stepActiveStyle = lipgloss.NewStyle().
		Foreground(colorLavender).
		Bold(true)

	helpStyle = lipgloss.NewStyle().
		Foreground(colorOverlay).
		MarginTop(1)

	errorStyle = lipgloss.NewStyle().
		Foreground(colorRed).
		Bold(true).
		MarginTop(1)

	successStyle = lipgloss.NewStyle().
		Foreground(colorGreen).
		Bold(true)

	labelStyle = lipgloss.NewStyle().
		Foreground(colorSubtext).
		MarginBottom(0)

	focusedInputStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colorLavender).
		Padding(0, 1)

	blurredInputStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colorOverlay).
		Padding(0, 1)

	selectedStyle = lipgloss.NewStyle().
		Foreground(colorLavender).
		Bold(true)

	normalStyle = lipgloss.NewStyle().
		Foreground(colorText)

	checkedStyle = lipgloss.NewStyle().
		Foreground(colorGreen)

	dimStyle = lipgloss.NewStyle().
		Foreground(colorSubtext)

	codeStyle = lipgloss.NewStyle().
		Foreground(colorTeal).
		Background(colorSurface).
		Padding(0, 1)

	panelStyle = lipgloss.NewStyle().
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(colorOverlay).
		Padding(1, 2).
		MarginTop(1)
)
