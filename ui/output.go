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
	lines        []outputLine
	pending      string
	pendingErr   bool
	status       string
	inputBuffer  []rune
	inputFocus   bool
	selecting    bool
	selStartLine int
	selStartCol  int
	selEndLine   int
	selEndCol    int
	width        int
	height       int
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
	m.ClearSelection()
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

func (m *outputModel) ClearSelection() {
	m.selecting = false
	m.selStartLine = 0
	m.selStartCol = 0
	m.selEndLine = 0
	m.selEndCol = 0
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

func (m *outputModel) BeginSelection(viewRow, col int) {
	lines, start := m.visibleOutputLines()
	if viewRow < 0 || viewRow >= len(lines) {
		m.ClearSelection()
		return
	}
	lineIndex := start + viewRow
	col = clamp(col, 0, len([]rune(lines[viewRow].text)))
	m.selecting = true
	m.selStartLine, m.selStartCol = lineIndex, col
	m.selEndLine, m.selEndCol = lineIndex, col
}

func (m *outputModel) UpdateSelection(viewRow, col int) {
	if !m.selecting {
		return
	}
	lines, start := m.visibleOutputLines()
	if len(lines) == 0 {
		return
	}
	if viewRow < 0 {
		viewRow = 0
	}
	if viewRow >= len(lines) {
		viewRow = len(lines) - 1
	}
	lineIndex := start + viewRow
	col = clamp(col, 0, len([]rune(lines[viewRow].text)))
	m.selEndLine, m.selEndCol = lineIndex, col
}

func (m *outputModel) EndSelection() {
	m.selecting = false
}

func (m *outputModel) HasSelection() bool {
	return m.selStartLine != m.selEndLine || m.selStartCol != m.selEndCol
}

func (m *outputModel) SelectedText() string {
	if !m.HasSelection() {
		return ""
	}
	lines, _ := m.visibleOutputLines()
	all := m.allOutputLines()
	if len(lines) == 0 || len(all) == 0 {
		return ""
	}
	startLine, startCol, endLine, endCol := normalizedSelection(m.selStartLine, m.selStartCol, m.selEndLine, m.selEndCol)
	var parts []string
	for i := startLine; i <= endLine && i < len(all); i++ {
		runes := []rune(all[i].text)
		from := 0
		to := len(runes)
		if i == startLine {
			from = clamp(startCol, 0, len(runes))
		}
		if i == endLine {
			to = clamp(endCol, 0, len(runes))
		}
		if from > to {
			from = to
		}
		parts = append(parts, string(runes[from:to]))
	}
	return strings.Join(parts, "\n")
}

func (m outputModel) View() string {
	var body []string
	body = append(body, accentStyle.Render("Live Output")+"  "+mutedStyle.Render(m.status))

	visibleLines, start := m.visibleOutputLines()
	for i, line := range visibleLines {
		body = append(body, m.renderOutputLine(start+i, line))
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

func (m outputModel) allOutputLines() []outputLine {
	lines := make([]outputLine, 0, len(m.lines)+1)
	lines = append(lines, m.lines...)
	if m.pending != "" {
		lines = append(lines, outputLine{text: m.pending, isErr: m.pendingErr})
	}
	return lines
}

func (m outputModel) visibleOutputLines() ([]outputLine, int) {
	available := max(1, m.height-4)
	lines := m.allOutputLines()
	start := 0
	if len(lines) > available {
		start = len(lines) - available
	}
	return lines[start:], start
}

func (m outputModel) visibleOutputLineCount() int {
	lines, _ := m.visibleOutputLines()
	if len(lines) == 0 {
		return 1
	}
	return len(lines)
}

func (m outputModel) inputViewRow() int {
	return 2 + m.visibleOutputLineCount()
}

func (m outputModel) renderOutputLine(absIndex int, line outputLine) string {
	base := lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	if line.isErr {
		base = errorStyle
	}
	if !m.HasSelection() {
		return base.Render(line.text)
	}
	startLine, startCol, endLine, endCol := normalizedSelection(m.selStartLine, m.selStartCol, m.selEndLine, m.selEndCol)
	if absIndex < startLine || absIndex > endLine {
		return base.Render(line.text)
	}
	runes := []rune(line.text)
	from := 0
	to := len(runes)
	if absIndex == startLine {
		from = clamp(startCol, 0, len(runes))
	}
	if absIndex == endLine {
		to = clamp(endCol, 0, len(runes))
	}
	if from > to {
		from = to
	}
	selected := lipgloss.NewStyle().Background(lipgloss.Color("12")).Foreground(lipgloss.Color("15"))
	return base.Render(string(runes[:from])) + selected.Render(string(runes[from:to])) + base.Render(string(runes[to:]))
}

func normalizedSelection(startLine, startCol, endLine, endCol int) (int, int, int, int) {
	if startLine < endLine || (startLine == endLine && startCol <= endCol) {
		return startLine, startCol, endLine, endCol
	}
	return endLine, endCol, startLine, startCol
}

func clamp(v, low, high int) int {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}
