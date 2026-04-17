package editor

import (
	"fmt"
	"strings"

	"github.com/alecthomas/chroma"
	"github.com/alecthomas/chroma/lexers"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ChangedMsg struct{}

type Model struct {
	path      string
	lines     []string
	cursorRow int
	cursorCol int
	rowOffset int
	colOffset int
	width     int
	height    int
	dirty     bool
}

func New() Model {
	return Model{lines: []string{""}}
}

func (m *Model) SetSize(width, height int) {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	m.width = width
	m.height = height
	m.clampCursor()
	m.ensureCursorVisible()
}

func (m *Model) LoadFile(path, content string) {
	m.path = path
	m.lines = strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	if len(m.lines) == 0 {
		m.lines = []string{""}
	}
	m.cursorRow = 0
	m.cursorCol = 0
	m.rowOffset = 0
	m.colOffset = 0
	m.dirty = false
}

func (m Model) Path() string {
	return m.path
}

func (m Model) Dirty() bool {
	return m.dirty
}

func (m Model) CurrentLine() string {
	if len(m.lines) == 0 || m.cursorRow < 0 || m.cursorRow >= len(m.lines) {
		return ""
	}
	return m.lines[m.cursorRow]
}

func (m Model) Content() string {
	return strings.Join(m.lines, "\n")
}

func (m *Model) PasteText(text string) {
	if text == "" {
		return
	}
	m.insertText(strings.ReplaceAll(text, "\r\n", "\n"))
	m.dirty = true
	m.clampCursor()
	m.ensureCursorVisible()
}

func (m *Model) SetCursorFromView(row, col int) {
	lineNumberWidth := max(3, len(fmt.Sprintf("%d", len(m.lines))))
	contentCol := col - (lineNumberWidth + 3)
	if contentCol < 0 {
		contentCol = 0
	}

	m.cursorRow = m.rowOffset + row
	m.cursorCol = m.colOffset + contentCol
	m.clampCursor()
	m.ensureCursorVisible()
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	changed := false

	switch key.String() {
	case "up":
		m.cursorRow--
	case "down":
		m.cursorRow++
	case "left":
		if m.cursorCol > 0 {
			m.cursorCol--
		} else if m.cursorRow > 0 {
			m.cursorRow--
			m.cursorCol = runeCount(m.lines[m.cursorRow])
		}
	case "right":
		if m.cursorCol < runeCount(m.lines[m.cursorRow]) {
			m.cursorCol++
		} else if m.cursorRow < len(m.lines)-1 {
			m.cursorRow++
			m.cursorCol = 0
		}
	case "home":
		m.cursorCol = 0
	case "end":
		m.cursorCol = runeCount(m.lines[m.cursorRow])
	case "pgup":
		m.cursorRow -= max(1, m.height-1)
	case "pgdown":
		m.cursorRow += max(1, m.height-1)
	case "backspace":
		changed = m.deleteBackward()
	case "delete":
		changed = m.deleteForward()
	case "enter":
		changed = m.insertNewline()
	case "tab":
		m.insertText("    ")
		changed = true
	default:
		if key.Type == tea.KeyRunes || key.Type == tea.KeySpace {
			m.insertText(key.String())
			changed = true
		}
	}

	m.clampCursor()
	m.ensureCursorVisible()

	if changed {
		m.dirty = true
		return m, func() tea.Msg { return ChangedMsg{} }
	}

	return m, nil
}

func (m Model) View() string {
	if m.width < 8 || m.height < 2 {
		return "Terminal too small for editor"
	}

	lineNumberWidth := max(3, len(fmt.Sprintf("%d", len(m.lines))))
	contentWidth := max(1, m.width-lineNumberWidth-3)

	var rendered []string
	for i := 0; i < m.height; i++ {
		lineIndex := m.rowOffset + i
		number := " "
		text := ""
		if lineIndex < len(m.lines) {
			number = fmt.Sprintf("%*d", lineNumberWidth, lineIndex+1)
			text = m.renderLine(lineIndex, contentWidth)
		} else {
			number = strings.Repeat(" ", lineNumberWidth)
		}

		numStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
		if lineIndex == m.cursorRow {
			numStyle = numStyle.Foreground(lipgloss.Color("14")).Bold(true)
		}

		rendered = append(rendered, numStyle.Render(number)+" │ "+text)
	}

	return strings.Join(rendered, "\n")
}

func (m Model) renderLine(lineIndex, contentWidth int) string {
	line := m.lines[lineIndex]
	chunks := highlightLine(m.path, line)
	if lineIndex == m.cursorRow {
		return renderChunksWithCursor(chunks, m.colOffset, contentWidth, m.cursorCol)
	}
	return renderChunks(chunks, m.colOffset, contentWidth)
}

func (m *Model) clampCursor() {
	if len(m.lines) == 0 {
		m.lines = []string{""}
	}
	if m.cursorRow < 0 {
		m.cursorRow = 0
	}
	if m.cursorRow >= len(m.lines) {
		m.cursorRow = len(m.lines) - 1
	}
	lineWidth := runeCount(m.lines[m.cursorRow])
	if m.cursorCol < 0 {
		m.cursorCol = 0
	}
	if m.cursorCol > lineWidth {
		m.cursorCol = lineWidth
	}
}

func (m *Model) ensureCursorVisible() {
	if m.cursorRow < m.rowOffset {
		m.rowOffset = m.cursorRow
	}
	if m.cursorRow >= m.rowOffset+m.height {
		m.rowOffset = m.cursorRow - m.height + 1
	}
	if m.cursorCol < m.colOffset {
		m.colOffset = m.cursorCol
	}
	viewWidth := max(1, m.width-8)
	if m.cursorCol >= m.colOffset+viewWidth {
		m.colOffset = m.cursorCol - viewWidth + 1
	}
	if m.rowOffset < 0 {
		m.rowOffset = 0
	}
	if m.colOffset < 0 {
		m.colOffset = 0
	}
}

func (m *Model) insertText(text string) {
	parts := strings.Split(text, "\n")
	if len(parts) == 1 {
		line := m.lines[m.cursorRow]
		left, right := splitAtRune(line, m.cursorCol)
		m.lines[m.cursorRow] = left + text + right
		m.cursorCol += runeCount(text)
		return
	}

	current := m.lines[m.cursorRow]
	left, right := splitAtRune(current, m.cursorCol)
	head := left + parts[0]
	tail := parts[len(parts)-1] + right
	middle := parts[1 : len(parts)-1]

	newLines := append([]string{}, m.lines[:m.cursorRow]...)
	newLines = append(newLines, head)
	newLines = append(newLines, middle...)
	newLines = append(newLines, tail)
	newLines = append(newLines, m.lines[m.cursorRow+1:]...)
	m.lines = newLines
	m.cursorRow += len(parts) - 1
	m.cursorCol = runeCount(parts[len(parts)-1])
}

func (m *Model) MarkSaved() {
	m.dirty = false
}

func (m *Model) insertNewline() bool {
	line := m.lines[m.cursorRow]
	left, right := splitAtRune(line, m.cursorCol)
	m.lines[m.cursorRow] = left
	next := append([]string{right}, m.lines[m.cursorRow+1:]...)
	m.lines = append(m.lines[:m.cursorRow+1], next...)
	m.cursorRow++
	m.cursorCol = 0
	return true
}

func (m *Model) deleteBackward() bool {
	if m.cursorCol > 0 {
		line := m.lines[m.cursorRow]
		m.lines[m.cursorRow] = removeRuneAt(line, m.cursorCol-1)
		m.cursorCol--
		return true
	}
	if m.cursorRow == 0 {
		return false
	}
	prev := m.lines[m.cursorRow-1]
	current := m.lines[m.cursorRow]
	m.cursorCol = runeCount(prev)
	m.lines[m.cursorRow-1] = prev + current
	m.lines = append(m.lines[:m.cursorRow], m.lines[m.cursorRow+1:]...)
	m.cursorRow--
	return true
}

func (m *Model) deleteForward() bool {
	line := m.lines[m.cursorRow]
	if m.cursorCol < runeCount(line) {
		m.lines[m.cursorRow] = removeRuneAt(line, m.cursorCol)
		return true
	}
	if m.cursorRow >= len(m.lines)-1 {
		return false
	}
	m.lines[m.cursorRow] += m.lines[m.cursorRow+1]
	m.lines = append(m.lines[:m.cursorRow+1], m.lines[m.cursorRow+2:]...)
	return true
}

func highlightLine(path, line string) []styledChunk {
	lexer := lexers.Match(path)
	if lexer == nil {
		lexer = lexers.Analyse(line)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}

	iterator, err := lexer.Tokenise(nil, line)
	if err != nil {
		return []styledChunk{{text: line, style: styleForToken(chroma.Text)}}
	}

	chunks := make([]styledChunk, 0, 8)
	for token := iterator(); token != chroma.EOF; token = iterator() {
		if token.Value == "" {
			continue
		}
		chunks = append(chunks, styledChunk{
			text:  token.Value,
			style: styleForToken(token.Type),
		})
	}
	return chunks
}

type styledChunk struct {
	text  string
	style lipgloss.Style
}

func renderChunks(chunks []styledChunk, offset, width int) string {
	remainingOffset := offset
	remainingWidth := width
	var b strings.Builder

	for _, chunk := range chunks {
		if remainingWidth <= 0 {
			break
		}

		runes := []rune(chunk.text)
		if remainingOffset >= len(runes) {
			remainingOffset -= len(runes)
			continue
		}

		start := remainingOffset
		end := min(len(runes), start+remainingWidth)
		b.WriteString(chunk.style.Render(string(runes[start:end])))
		remainingWidth -= end - start
		remainingOffset = 0
	}

	if remainingWidth > 0 {
		b.WriteString(strings.Repeat(" ", remainingWidth))
	}

	return b.String()
}

func renderChunksWithCursor(chunks []styledChunk, offset, width, cursorCol int) string {
	cursorStyle := lipgloss.NewStyle().Background(lipgloss.Color("12")).Foreground(lipgloss.Color("15"))
	remainingOffset := offset
	remainingWidth := width
	visibleCol := 0
	var b strings.Builder

	for _, chunk := range chunks {
		if remainingWidth <= 0 {
			break
		}

		runes := []rune(chunk.text)
		if remainingOffset >= len(runes) {
			remainingOffset -= len(runes)
			continue
		}

		start := remainingOffset
		end := min(len(runes), start+remainingWidth)
		for _, r := range runes[start:end] {
			part := string(r)
			if offset+visibleCol == cursorCol {
				b.WriteString(cursorStyle.Render(part))
			} else {
				b.WriteString(chunk.style.Render(part))
			}
			visibleCol++
			remainingWidth--
			if remainingWidth <= 0 {
				break
			}
		}
		remainingOffset = 0
	}

	if offset+visibleCol == cursorCol && remainingWidth > 0 {
		b.WriteString(cursorStyle.Render(" "))
		remainingWidth--
	}
	if remainingWidth > 0 {
		b.WriteString(strings.Repeat(" ", remainingWidth))
	}
	return b.String()
}

func styleForToken(tokenType chroma.TokenType) lipgloss.Style {
	switch {
	case tokenType.InCategory(chroma.Keyword):
		return lipgloss.NewStyle().Foreground(lipgloss.Color("141")).Bold(true)
	case tokenType.InCategory(chroma.String):
		return lipgloss.NewStyle().Foreground(lipgloss.Color("221"))
	case tokenType.InCategory(chroma.Comment):
		return lipgloss.NewStyle().Foreground(lipgloss.Color("108")).Italic(true)
	case tokenType.InCategory(chroma.Number):
		return lipgloss.NewStyle().Foreground(lipgloss.Color("117"))
	case tokenType.InCategory(chroma.Operator):
		return lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	case tokenType == chroma.NameFunction || tokenType == chroma.NameBuiltin:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func splitAtRune(s string, idx int) (string, string) {
	runes := []rune(s)
	idx = min(max(idx, 0), len(runes))
	return string(runes[:idx]), string(runes[idx:])
}

func removeRuneAt(s string, idx int) string {
	runes := []rune(s)
	if idx < 0 || idx >= len(runes) {
		return s
	}
	return string(append(runes[:idx], runes[idx+1:]...))
}

func runeCount(s string) int {
	return len([]rune(s))
}
