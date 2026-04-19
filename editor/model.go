package editor

import (
	"fmt"
	"strings"

	"github.com/alecthomas/chroma"
	"github.com/alecthomas/chroma/lexers"
	"github.com/alecthomas/chroma/styles"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ChangedMsg struct{}

var activeThemeName = "monokai"

type selectionMode int

const (
	selectionLinear selectionMode = iota
	selectionBlock
)

type snapshot struct {
	lines         []string
	cursorRow     int
	cursorCol     int
	rowOffset     int
	colOffset     int
	selectionMode selectionMode
	selStartRow   int
	selStartCol   int
	selEndRow     int
	selEndCol     int
}

type Model struct {
	path          string
	lines         []string
	cursorRow     int
	cursorCol     int
	rowOffset     int
	colOffset     int
	width         int
	height        int
	dirty         bool
	execLine      int
	execWaiting   bool
	selecting     bool
	selStartRow   int
	selStartCol   int
	selEndRow     int
	selEndCol     int
	selectionMode selectionMode
	undoStack     []snapshot
	redoStack     []snapshot
}

func New() Model {
	return Model{lines: []string{""}}
}

func SetTheme(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "monokai"
	}
	activeThemeName = name
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
	m.execLine = 0
	m.execWaiting = false
	m.selectionMode = selectionLinear
	m.undoStack = nil
	m.redoStack = nil
	m.ClearSelection()
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

func (m Model) CurrentCursorLine() int {
	return m.cursorRow + 1
}

func (m Model) HasSelection() bool {
	return m.selStartRow != m.selEndRow || m.selStartCol != m.selEndCol
}

func (m *Model) ClearSelection() {
	m.selecting = false
	m.selStartRow = 0
	m.selStartCol = 0
	m.selEndRow = 0
	m.selEndCol = 0
}

func (m Model) BlockSelectionEnabled() bool {
	return m.selectionMode == selectionBlock
}

func (m *Model) ToggleBlockSelection() bool {
	if m.selectionMode == selectionBlock {
		m.selectionMode = selectionLinear
		return false
	}
	m.selectionMode = selectionBlock
	return true
}

func (m Model) SelectedText() string {
	if !m.HasSelection() {
		return ""
	}
	startRow, startCol, endRow, endCol := normalizeSelection(m.selStartRow, m.selStartCol, m.selEndRow, m.selEndCol)
	if startRow < 0 || startRow >= len(m.lines) {
		return ""
	}
	if endRow >= len(m.lines) {
		endRow = len(m.lines) - 1
	}
	parts := make([]string, 0, endRow-startRow+1)
	for row := startRow; row <= endRow; row++ {
		runes := []rune(m.lines[row])
		from := 0
		to := len(runes)
		if m.selectionMode == selectionBlock {
			from = clamp(startCol, 0, len(runes))
			to = clamp(endCol, 0, len(runes))
		} else {
			if row == startRow {
				from = clamp(startCol, 0, len(runes))
			}
			if row == endRow {
				to = clamp(endCol, 0, len(runes))
			}
		}
		if from > to {
			from = to
		}
		parts = append(parts, string(runes[from:to]))
	}
	return strings.Join(parts, "\n")
}

func (m Model) Content() string {
	return strings.Join(m.lines, "\n")
}

func (m *Model) PasteText(text string) {
	if text == "" {
		return
	}
	m.pushUndo()
	m.ClearSelection()
	m.insertText(strings.ReplaceAll(text, "\r\n", "\n"))
	m.dirty = true
	m.clampCursor()
	m.ensureCursorVisible()
}

func (m *Model) SetCursorFromView(row, col int) {
	m.setCursorFromView(row, col)
	m.ClearSelection()
}

func (m *Model) BeginSelectionFromView(row, col int) {
	m.setCursorFromView(row, col)
	m.selecting = true
	m.selStartRow = m.cursorRow
	m.selStartCol = m.cursorCol
	m.selEndRow = m.cursorRow
	m.selEndCol = m.cursorCol
}

func (m *Model) UpdateSelectionFromView(row, col int) {
	if !m.selecting {
		return
	}
	m.setCursorFromView(row, col)
	m.selEndRow = m.cursorRow
	m.selEndCol = m.cursorCol
}

func (m *Model) EndSelection() {
	m.selecting = false
}

func (m *Model) setCursorFromView(row, col int) {
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

func (m *Model) Scroll(delta int) {
	if delta == 0 {
		return
	}

	maxOffset := max(0, len(m.lines)-m.height)
	m.rowOffset += delta
	if m.rowOffset < 0 {
		m.rowOffset = 0
	}
	if m.rowOffset > maxOffset {
		m.rowOffset = maxOffset
	}

	if m.cursorRow < m.rowOffset {
		m.cursorRow = m.rowOffset
	}
	if m.cursorRow >= m.rowOffset+m.height {
		m.cursorRow = m.rowOffset + m.height - 1
	}
	m.clampCursor()
}

func (m *Model) SetExecution(line int, waiting bool) {
	m.execLine = line
	m.execWaiting = waiting
	if line > 0 {
		m.RevealLine(line)
	}
}

func (m Model) ExecutionLine() int {
	return m.execLine
}

func (m *Model) ClearExecution() {
	m.execLine = 0
	m.execWaiting = false
}

func (m *Model) RevealLine(line int) {
	if line <= 0 {
		return
	}
	target := line - 1
	if target < m.rowOffset {
		m.rowOffset = target
	}
	if target >= m.rowOffset+m.height {
		m.rowOffset = target - m.height + 1
	}
	if m.rowOffset < 0 {
		m.rowOffset = 0
	}
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
		changed = m.insertLiteral("    ")
	default:
		if key.Type == tea.KeyRunes || key.Type == tea.KeySpace {
			changed = m.insertRune(key.String())
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
	contentWidth := max(1, m.width-lineNumberWidth-4)

	var rendered []string
	for i := 0; i < m.height; i++ {
		lineIndex := m.rowOffset + i
		number := " "
		text := ""
		if lineIndex < len(m.lines) {
			marker := " "
			if m.execLine == lineIndex+1 {
				if m.execWaiting {
					marker = "●"
				} else {
					marker = "▶"
				}
			}
			number = fmt.Sprintf("%s%*d", marker, lineNumberWidth, lineIndex+1)
			text = m.renderLine(lineIndex, contentWidth)
		} else {
			number = strings.Repeat(" ", lineNumberWidth+1)
		}

		numStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
		if lineIndex == m.cursorRow {
			numStyle = numStyle.Foreground(lipgloss.Color("14")).Bold(true)
		} else if m.execLine == lineIndex+1 {
			if m.execWaiting {
				numStyle = numStyle.Foreground(lipgloss.Color("11")).Bold(true)
			} else {
				numStyle = numStyle.Foreground(lipgloss.Color("10")).Bold(true)
			}
		}

		if m.execLine == lineIndex+1 && lineIndex != m.cursorRow {
			bg := lipgloss.NewStyle().Background(lipgloss.Color("236"))
			if m.execWaiting {
				bg = lipgloss.NewStyle().Background(lipgloss.Color("58"))
			}
			text = bg.Render(text)
		}

		rendered = append(rendered, numStyle.Render(number)+" │ "+text)
	}

	return strings.Join(rendered, "\n")
}

func (m Model) renderLine(lineIndex, contentWidth int) string {
	line := m.lines[lineIndex]
	chunks := highlightLine(m.path, line)
	startCol, endCol, selected := m.selectionForLine(lineIndex)
	return renderChunks(chunks, m.colOffset, contentWidth, cursorRender{
		enabled: lineIndex == m.cursorRow,
		col:     m.cursorCol,
	}, selectionRender{
		enabled: selected,
		start:   startCol,
		end:     endCol,
	})
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

func (m *Model) insertLiteral(text string) bool {
	m.pushUndo()
	m.ClearSelection()
	m.insertText(text)
	return true
}

func (m *Model) MarkSaved() {
	m.dirty = false
}

func (m *Model) insertNewline() bool {
	m.pushUndo()
	m.ClearSelection()
	line := m.lines[m.cursorRow]
	left, right := splitAtRune(line, m.cursorCol)
	indent := leadingWhitespace(left)
	if shouldIncreaseIndent(left) {
		indent += "    "
	}
	m.lines[m.cursorRow] = left
	next := append([]string{indent + right}, m.lines[m.cursorRow+1:]...)
	m.lines = append(m.lines[:m.cursorRow+1], next...)
	m.cursorRow++
	m.cursorCol = runeCount(indent)
	return true
}

func (m *Model) deleteBackward() bool {
	m.pushUndo()
	m.ClearSelection()
	if m.cursorCol > 0 {
		line := m.lines[m.cursorRow]
		if pairStart, ok := emptyPairBeforeCursor(line, m.cursorCol); ok {
			runes := []rune(line)
			m.lines[m.cursorRow] = string(append(runes[:pairStart], runes[m.cursorCol+1:]...))
			m.cursorCol--
			return true
		}
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
	m.pushUndo()
	m.ClearSelection()
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

func (m *Model) Undo() bool {
	if len(m.undoStack) == 0 {
		return false
	}
	m.redoStack = append(m.redoStack, m.snapshot())
	prev := m.undoStack[len(m.undoStack)-1]
	m.undoStack = m.undoStack[:len(m.undoStack)-1]
	m.restore(prev)
	m.dirty = true
	return true
}

func (m *Model) Redo() bool {
	if len(m.redoStack) == 0 {
		return false
	}
	m.undoStack = append(m.undoStack, m.snapshot())
	next := m.redoStack[len(m.redoStack)-1]
	m.redoStack = m.redoStack[:len(m.redoStack)-1]
	m.restore(next)
	m.dirty = true
	return true
}

func (m *Model) FindNext(query string, forward bool) bool {
	queryRunes := []rune(query)
	if len(queryRunes) == 0 || len(m.lines) == 0 {
		return false
	}

	if forward {
		for row := m.cursorRow; row < len(m.lines); row++ {
			start := 0
			if row == m.cursorRow {
				start = min(runeCount(m.lines[row]), m.cursorCol)
			}
			if col := findInRunes([]rune(m.lines[row]), queryRunes, start, true); col >= 0 {
				m.applyMatch(row, col, len(queryRunes))
				return true
			}
		}
		for row := 0; row <= m.cursorRow && row < len(m.lines); row++ {
			end := runeCount(m.lines[row])
			if row == m.cursorRow {
				end = min(end, m.cursorCol)
			}
			if col := findInRunes([]rune(m.lines[row]), queryRunes, end, false); col >= 0 {
				m.applyMatch(row, col, len(queryRunes))
				return true
			}
		}
		return false
	}

	for row := m.cursorRow; row >= 0; row-- {
		end := runeCount(m.lines[row])
		if row == m.cursorRow {
			end = min(end, m.cursorCol)
		}
		if col := findInRunes([]rune(m.lines[row]), queryRunes, end, false); col >= 0 {
			m.applyMatch(row, col, len(queryRunes))
			return true
		}
	}
	for row := len(m.lines) - 1; row >= m.cursorRow; row-- {
		start := 0
		if row == m.cursorRow {
			start = min(runeCount(m.lines[row]), m.cursorCol)
		}
		if col := findInRunes([]rune(m.lines[row]), queryRunes, start, true); col >= 0 {
			m.applyMatch(row, col, len(queryRunes))
			return true
		}
	}
	return false
}

func (m *Model) ReplaceSelection(find, replace string) bool {
	if !m.HasSelection() || m.selectionMode == selectionBlock || m.SelectedText() != find {
		return false
	}
	m.pushUndo()
	startRow, startCol, endRow, endCol := normalizeSelection(m.selStartRow, m.selStartCol, m.selEndRow, m.selEndCol)
	left, _ := splitAtRune(m.lines[startRow], startCol)
	_, right := splitAtRune(m.lines[endRow], endCol)
	m.lines = append(append(append([]string{}, m.lines[:startRow]...), left+replace+right), m.lines[endRow+1:]...)
	m.cursorRow = startRow
	m.cursorCol = startCol + runeCount(replace)
	m.selStartRow, m.selStartCol = startRow, startCol
	m.selEndRow, m.selEndCol = startRow, m.cursorCol
	m.ensureCursorVisible()
	return true
}

func (m *Model) ReplaceAll(find, replace string) int {
	if find == "" {
		return 0
	}
	count := 0
	lines := make([]string, len(m.lines))
	for i, line := range m.lines {
		replaced := strings.ReplaceAll(line, find, replace)
		if replaced != line {
			count += strings.Count(line, find)
		}
		lines[i] = replaced
	}
	if count == 0 {
		return 0
	}
	m.pushUndo()
	m.lines = lines
	m.ClearSelection()
	m.clampCursor()
	m.ensureCursorVisible()
	return count
}

func (m *Model) JumpToLine(line int) bool {
	if line <= 0 || line > len(m.lines) {
		return false
	}
	m.cursorRow = line - 1
	m.cursorCol = min(m.cursorCol, runeCount(m.lines[m.cursorRow]))
	m.ClearSelection()
	m.ensureCursorVisible()
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

type cursorRender struct {
	enabled bool
	col     int
}

type selectionRender struct {
	enabled bool
	start   int
	end     int
}

func renderChunks(chunks []styledChunk, offset, width int, cursor cursorRender, selection selectionRender) string {
	cursorStyle := lipgloss.NewStyle().Background(lipgloss.Color("12")).Foreground(lipgloss.Color("15"))
	selectionStyle := lipgloss.NewStyle().Background(lipgloss.Color("13")).Foreground(lipgloss.Color("15"))
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
			absoluteCol := offset + visibleCol
			style := chunk.style
			switch {
			case selection.enabled && absoluteCol >= selection.start && absoluteCol < selection.end:
				style = style.Background(lipgloss.Color("13")).Foreground(lipgloss.Color("15"))
			case cursor.enabled && absoluteCol == cursor.col:
				style = cursorStyle
			}
			b.WriteString(style.Render(part))
			visibleCol++
			remainingWidth--
			if remainingWidth <= 0 {
				break
			}
		}
		remainingOffset = 0
	}

	if cursor.enabled && offset+visibleCol == cursor.col && remainingWidth > 0 {
		style := cursorStyle
		if selection.enabled && cursor.col >= selection.start && cursor.col < selection.end {
			style = selectionStyle
		}
		b.WriteString(style.Render(" "))
		remainingWidth--
	}
	if remainingWidth > 0 {
		b.WriteString(strings.Repeat(" ", remainingWidth))
	}

	return b.String()
}

func (m Model) selectionForLine(lineIndex int) (int, int, bool) {
	if !m.HasSelection() {
		return 0, 0, false
	}
	startRow, startCol, endRow, endCol := normalizeSelection(m.selStartRow, m.selStartCol, m.selEndRow, m.selEndCol)
	if lineIndex < startRow || lineIndex > endRow {
		return 0, 0, false
	}
	lineLen := runeCount(m.lines[lineIndex])
	start := 0
	end := lineLen
	if m.selectionMode == selectionBlock {
		start = clamp(startCol, 0, lineLen)
		end = clamp(endCol, 0, lineLen)
	} else {
		if lineIndex == startRow {
			start = clamp(startCol, 0, lineLen)
		}
		if lineIndex == endRow {
			end = clamp(endCol, 0, lineLen)
		}
	}
	if start > end {
		start = end
	}
	return start, end, true
}

func styleForToken(tokenType chroma.TokenType) lipgloss.Style {
	return lipglossStyleForEntry(styles.Get(activeThemeName).Get(tokenType))
}

func lipglossStyleForEntry(entry chroma.StyleEntry) lipgloss.Style {
	style := lipgloss.NewStyle()
	if entry.Colour.IsSet() {
		style = style.Foreground(lipgloss.Color(entry.Colour.String()))
	}
	if entry.Background.IsSet() {
		style = style.Background(lipgloss.Color(entry.Background.String()))
	}
	if entry.Bold == chroma.Yes {
		style = style.Bold(true)
	}
	if entry.Italic == chroma.Yes {
		style = style.Italic(true)
	}
	if entry.Underline == chroma.Yes {
		style = style.Underline(true)
	}
	return style
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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

func normalizeSelection(startRow, startCol, endRow, endCol int) (int, int, int, int) {
	if startRow < endRow || (startRow == endRow && startCol <= endCol) {
		return startRow, startCol, endRow, endCol
	}
	return endRow, endCol, startRow, startCol
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

func (m *Model) pushUndo() {
	m.undoStack = append(m.undoStack, m.snapshot())
	if len(m.undoStack) > 200 {
		m.undoStack = m.undoStack[len(m.undoStack)-200:]
	}
	m.redoStack = nil
}

func (m Model) snapshot() snapshot {
	lines := make([]string, len(m.lines))
	copy(lines, m.lines)
	return snapshot{
		lines:         lines,
		cursorRow:     m.cursorRow,
		cursorCol:     m.cursorCol,
		rowOffset:     m.rowOffset,
		colOffset:     m.colOffset,
		selectionMode: m.selectionMode,
		selStartRow:   m.selStartRow,
		selStartCol:   m.selStartCol,
		selEndRow:     m.selEndRow,
		selEndCol:     m.selEndCol,
	}
}

func (m *Model) restore(s snapshot) {
	m.lines = append([]string{}, s.lines...)
	m.cursorRow = s.cursorRow
	m.cursorCol = s.cursorCol
	m.rowOffset = s.rowOffset
	m.colOffset = s.colOffset
	m.selectionMode = s.selectionMode
	m.selStartRow = s.selStartRow
	m.selStartCol = s.selStartCol
	m.selEndRow = s.selEndRow
	m.selEndCol = s.selEndCol
	m.clampCursor()
	m.ensureCursorVisible()
}

func (m *Model) insertRune(value string) bool {
	if value == "" {
		return false
	}
	r := []rune(value)[0]
	if closer, ok := autoClosePair(r); ok {
		m.pushUndo()
		m.ClearSelection()
		m.insertText(string([]rune{r, closer}))
		m.cursorCol--
		return true
	}
	if shouldSkipCloser(r, m.lines[m.cursorRow], m.cursorCol) {
		m.ClearSelection()
		m.cursorCol++
		return false
	}
	return m.insertLiteral(value)
}

func (m *Model) applyMatch(row, col, width int) {
	m.cursorRow = row
	m.cursorCol = col + width
	m.selectionMode = selectionLinear
	m.selStartRow, m.selStartCol = row, col
	m.selEndRow, m.selEndCol = row, col+width
	m.ensureCursorVisible()
}

func leadingWhitespace(s string) string {
	runes := []rune(s)
	idx := 0
	for idx < len(runes) && (runes[idx] == ' ' || runes[idx] == '\t') {
		idx++
	}
	return string(runes[:idx])
}

func shouldIncreaseIndent(s string) bool {
	trimmed := strings.TrimSpace(s)
	return strings.HasSuffix(trimmed, ":") || strings.HasSuffix(trimmed, "{") || strings.HasSuffix(trimmed, "[") || strings.HasSuffix(trimmed, "(")
}

func autoClosePair(r rune) (rune, bool) {
	switch r {
	case '(':
		return ')', true
	case '[':
		return ']', true
	case '{':
		return '}', true
	case '"':
		return '"', true
	case '\'':
		return '\'', true
	default:
		return 0, false
	}
}

func shouldSkipCloser(r rune, line string, col int) bool {
	switch r {
	case ')', ']', '}', '"', '\'':
	default:
		return false
	}
	runes := []rune(line)
	return col < len(runes) && runes[col] == r
}

func emptyPairBeforeCursor(line string, col int) (int, bool) {
	runes := []rune(line)
	if col <= 0 || col >= len(runes) {
		return 0, false
	}
	switch {
	case runes[col-1] == '(' && runes[col] == ')':
	case runes[col-1] == '[' && runes[col] == ']':
	case runes[col-1] == '{' && runes[col] == '}':
	case runes[col-1] == '"' && runes[col] == '"':
	case runes[col-1] == '\'' && runes[col] == '\'':
	default:
		return 0, false
	}
	return col - 1, true
}

func findInRunes(line, query []rune, anchor int, forward bool) int {
	if len(query) == 0 || len(line) < len(query) {
		return -1
	}
	if forward {
		for i := max(0, anchor); i <= len(line)-len(query); i++ {
			if string(line[i:i+len(query)]) == string(query) {
				return i
			}
		}
		return -1
	}
	limit := min(anchor-len(query), len(line)-len(query))
	for i := limit; i >= 0; i-- {
		if string(line[i:i+len(query)]) == string(query) {
			return i
		}
	}
	return -1
}
