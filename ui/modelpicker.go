package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type aiModelOption struct {
	provider string
	model    string
	detail   string
}

type modelPickerModel struct {
	items  []aiModelOption
	index  int
	width  int
	height int
}

func newModelPickerModel(items []aiModelOption, currentModel string) modelPickerModel {
	m := modelPickerModel{items: items}
	for i, item := range items {
		if item.model == currentModel {
			m.index = i
			break
		}
	}
	return m
}

func (m *modelPickerModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m modelPickerModel) Selected() (string, string) {
	if len(m.items) == 0 || m.index < 0 || m.index >= len(m.items) {
		return "", ""
	}
	return m.items[m.index].provider, m.items[m.index].model
}

func (m modelPickerModel) Update(msg tea.Msg) (modelPickerModel, tea.Cmd) {
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

func (m modelPickerModel) View() string {
	lines := []string{
		titleStyle.Render("Select AI Model"),
		mutedStyle.Render("Choose the AI provider and model for hints and explanations."),
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

	var lastProvider string
	for i, item := range m.items {
		if item.provider != lastProvider {
			if len(lines) > 3 {
				lines = append(lines, "")
			}
			lines = append(lines, mutedStyle.Render(modelGroupLabel(item.provider)))
			lastProvider = item.provider
		}
		lines = append(lines, renderItem(item.model, item.detail, i == m.index))
	}

	if len(m.items) == 0 {
		lines = append(lines, mutedStyle.Render("No AI models are currently available."))
	}

	lines = append(lines, "")
	lines = append(lines, mutedStyle.Render("↑/↓ move • enter select • esc cancel"))

	card := activePanelStyle.Padding(1, 2).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card)
}

func modelGroupLabel(provider string) string {
	switch provider {
	case "gemini":
		return "── Google Gemini (Free Tier) ──"
	case "openai":
		return "── OpenAI ──"
	default:
		return fmt.Sprintf("── %s ──", provider)
	}
}
