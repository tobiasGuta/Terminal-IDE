package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"terminal-ide/runner"
)

type interpreterPickerModel struct {
	items  []runner.PythonInterpreter
	index  int
	width  int
	height int
}

func newInterpreterPickerModel(items []runner.PythonInterpreter, current string) interpreterPickerModel {
	m := interpreterPickerModel{items: items}
	for i, item := range items {
		if item.Path == current || item.Command == current {
			m.index = i + 1
			break
		}
	}
	return m
}

func (m *interpreterPickerModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m interpreterPickerModel) SelectedCommand() string {
	if m.index == 0 {
		return ""
	}
	return m.items[m.index-1].Path
}

func (m interpreterPickerModel) Update(msg tea.Msg) (interpreterPickerModel, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	maxIndex := len(m.items)
	switch key.String() {
	case "up":
		if m.index > 0 {
			m.index--
		}
	case "down":
		if m.index < maxIndex {
			m.index++
		}
	}

	return m, nil
}

func (m interpreterPickerModel) View() string {
	lines := []string{
		titleStyle.Render("Select Python Interpreter"),
		mutedStyle.Render("Choose an installed interpreter or keep auto detection."),
		"",
	}

	renderItem := func(label, detail string, active bool) string {
		line := label
		if detail != "" {
			line += "  " + detail
		}
		style := lipgloss.NewStyle().Padding(0, 1).Width(40)
		if active {
			style = style.Background(lipgloss.Color("12")).Foreground(lipgloss.Color("15")).Bold(true)
		} else {
			style = style.Foreground(lipgloss.Color("252"))
		}
		return style.Render(line)
	}

	lines = append(lines, renderItem("auto", "Use detection + shebang/syntax hints", m.index == 0))
	for i, item := range m.items {
		detail := ""
		if strings.TrimSpace(item.Version) != "" {
			detail = fmt.Sprintf("Python %s", item.Version)
		}
		lines = append(lines, renderItem(item.Command, detail, m.index == i+1))
	}

	lines = append(lines, "")
	lines = append(lines, mutedStyle.Render("esc back • enter select • ↑↓ navigate"))

	card := activePanelStyle.Padding(1, 2).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card)
}
