package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type themeOption struct {
	name   string
	detail string
}

type themePickerModel struct {
	items  []themeOption
	index  int
	width  int
	height int
}

func newThemePickerModel(items []themeOption, currentTheme string) themePickerModel {
	m := themePickerModel{items: items}
	for i, item := range items {
		if item.name == currentTheme {
			m.index = i
			break
		}
	}
	return m
}

func (m *themePickerModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m themePickerModel) Selected() string {
	if len(m.items) == 0 || m.index < 0 || m.index >= len(m.items) {
		return ""
	}
	return m.items[m.index].name
}

func (m themePickerModel) Update(msg tea.Msg) (themePickerModel, tea.Cmd) {
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
		if m.index < len(m.items)-1 {
			m.index++
		}
	}

	return m, nil
}

func (m themePickerModel) View() string {
	lines := []string{
		titleStyle.Render("Select Theme"),
		mutedStyle.Render("Choose the syntax highlighting theme for the editor."),
		"",
	}

	renderItem := func(label, detail string, active bool) string {
		line := label
		if detail != "" {
			line += "  " + detail
		}
		style := lipgloss.NewStyle().Padding(0, 1).Width(44)
		if active {
			style = style.Background(lipgloss.Color("12")).Foreground(lipgloss.Color("15")).Bold(true)
		} else {
			style = style.Foreground(lipgloss.Color("252"))
		}
		return style.Render(line)
	}

	for i, item := range m.items {
		lines = append(lines, renderItem(item.name, item.detail, i == m.index))
	}

	if len(m.items) == 0 {
		lines = append(lines, mutedStyle.Render("No themes are currently available."))
	}

	lines = append(lines, "")
	lines = append(lines, mutedStyle.Render("↑/↓ move • enter select • esc cancel"))

	card := activePanelStyle.Padding(1, 2).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card)
}
