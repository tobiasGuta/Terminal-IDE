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
	pending     string
	pendingErr  bool
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
	m.pending = ""
	m.pendingErr = false
	m.status = status
}

func (m *outputModel) Append(text string, isErr bool) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if text == "" {
		return
	}

	if m.pending != "" && m.pendingErr != isErr {
		m.lines = append(m.lines, outputLine{text: m.pending, isErr: m.pendingErr})
		m.pending = ""
	}

	parts := strings.Split(text, "\n")
	for i, part := range parts {
		isLast := i == len(parts)-1
		if isLast {
			m.pending += part
			m.pendingErr = isErr
			continue
		}

		line := m.pending + part
		m.pending = ""
		m.pendingErr = isErr
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

func (m *outputModel) InputValue() string {
	return string(m.inputBuffer)
}

func (m *outputModel) PasteInput(text string) {
	if text == "" {
		return
	}
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\n", " ")
	m.inputBuffer = append(m.inputBuffer, []rune(text)...)
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

func (m *outputModel) EchoSubmittedInput(text string) {
	if m.pending != "" {
		m.lines = append(m.lines, outputLine{text: m.pending + text, isErr: m.pendingErr})
		m.pending = ""
		m.pendingErr = false
		return
	}
	m.lines = append(m.lines, outputLine{text: "> " + text, isErr: false})
}

func (m outputModel) View() string {
	var body []string
	body = append(body, accentStyle.Render("Live Output")+"  "+mutedStyle.Render(m.status))

	available := max(1, m.height-4)
	visibleLines := make([]outputLine, 0, len(m.lines)+1)
	visibleLines = append(visibleLines, m.lines...)
	if m.pending != "" {
		visibleLines = append(visibleLines, outputLine{text: m.pending, isErr: m.pendingErr})
	}
	start := 0
	if len(visibleLines) > available {
		start = len(visibleLines) - available
	}

	for _, line := range visibleLines[start:] {
		style := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
		if line.isErr {
			style = errorStyle
		}
		body = append(body, style.Render(line.text))
	}

	if len(visibleLines) == 0 {
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
