package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type welcomeModel struct {
	index   int
	options []string
	width   int
	height  int
}

func newWelcomeModel() welcomeModel {
	return welcomeModel{
		options: []string{"Open File", "Open Folder", "Recent Files (Soon)"},
	}
}

func (m *welcomeModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m welcomeModel) Selected() string {
	return m.options[m.index]
}

func (m welcomeModel) Update(msg tea.Msg) (welcomeModel, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch key.String() {
	case "up":
		if m.index > 0 {
			m.index--
		}
	case "down":
		if m.index < len(m.options)-1 {
			m.index++
		}
	}

	return m, nil
}

func (m welcomeModel) View() string {
	var items []string
	for i, option := range m.options {
		style := lipgloss.NewStyle().Padding(0, 1).Width(24)
		if i == m.index {
			style = style.Background(lipgloss.Color("12")).Foreground(lipgloss.Color("15")).Bold(true)
		} else if strings.Contains(option, "Soon") {
			style = style.Foreground(lipgloss.Color("8"))
		} else {
			style = style.Foreground(lipgloss.Color("14"))
		}
		items = append(items, style.Render(option))
	}

	logo := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Render("Terminal IDE")
	tagline := mutedStyle.Render("Bubble Tea + Lip Gloss + Chroma")
	body := lipgloss.JoinVertical(lipgloss.Center,
		logo,
		tagline,
		"",
		strings.Join(items, "\n"),
		"",
		mutedStyle.Render("↑/↓ move • enter select • ctrl+q quit"),
	)

	card := activePanelStyle.Padding(1, 3).Render(body)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card)
}
