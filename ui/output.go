package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type outputLine struct {
	text  string
	isErr bool
}

type outputModel struct {
	lines       []outputLine
	status      string
	inputBuffer []rune
	inputFocus  bool
	width       int
	height      int
}

func newOutputModel() outputModel {
	return outputModel{
		status: "Idle",
	}
}

func (m *outputModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *outputModel) Reset(status string) {
	m.lines = nil
	m.status = status
}

func (m *outputModel) Append(text string, isErr bool) {
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if line == "" {
			continue
		}
		m.lines = append(m.lines, outputLine{text: line, isErr: isErr})
	}
}

func (m *outputModel) SetStatus(status string) {
	m.status = status
}

func (m *outputModel) SetInputFocus(focused bool) {
	m.inputFocus = focused
}

func (m outputModel) InputFocused() bool {
	return m.inputFocus
}

func (m *outputModel) ClearInput() {
	m.inputBuffer = nil
}

func (m *outputModel) HandleKey(key string) (string, bool) {
	switch key {
	case "backspace":
		if len(m.inputBuffer) > 0 {
			m.inputBuffer = m.inputBuffer[:len(m.inputBuffer)-1]
		}
	case "enter":
		submitted := string(m.inputBuffer)
		m.inputBuffer = nil
		return submitted, true
	case "space":
		m.inputBuffer = append(m.inputBuffer, ' ')
	default:
		if len(key) == 1 {
			m.inputBuffer = append(m.inputBuffer, []rune(key)...)
		}
	}
	return "", false
}

func (m outputModel) View() string {
	var body []string
	body = append(body, accentStyle.Render("Live Output")+"  "+mutedStyle.Render(m.status))

	available := max(1, m.height-4)
	start := 0
	if len(m.lines) > available {
		start = len(m.lines) - available
	}

	for _, line := range m.lines[start:] {
		style := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
		if line.isErr {
			style = errorStyle
		}
		body = append(body, style.Render(line.text))
	}

	if len(m.lines) == 0 {
		body = append(body, mutedStyle.Render("Output will appear here after the next run."))
	}

	label := mutedStyle.Render("Input")
	if m.inputFocus {
		label = accentStyle.Render("Input")
	}
	body = append(body, "")
	body = append(body, label+": "+string(m.inputBuffer)+"_")

	return strings.Join(body, "\n")
}
