package ui

import "github.com/charmbracelet/lipgloss"

var (
	appPaddingStyle = lipgloss.NewStyle().Padding(1, 2)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("8")).
			Padding(0, 1)

	activePanelStyle = panelStyle.Copy().BorderForeground(lipgloss.Color("12"))

	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15"))
	mutedStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	accentStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
)
