package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Command struct {
	Label   string
	Keybind string
	Action  string
}

type commandPaletteMatch struct {
	command Command
	indices map[int]bool
}

type CommandPaletteActionMsg struct {
	Action string
}

type commandPaletteModel struct {
	title    string
	query    []rune
	index    int
	commands []Command
	filtered []commandPaletteMatch
	width    int
	height   int
}

func newCommandPaletteModel() commandPaletteModel {
	m := commandPaletteModel{
		title: "Command Palette",
		commands: []Command{
			{Label: "Save File", Keybind: "ctrl+s", Action: "save"},
			{Label: "Undo Edit", Keybind: "ctrl+z", Action: "undo"},
			{Label: "Redo Edit", Keybind: "ctrl+y", Action: "redo"},
			{Label: "Find and Replace", Keybind: "ctrl+f", Action: "find"},
			{Label: "Go to Line", Keybind: "ctrl+g", Action: "goto"},
			{Label: "Open File", Keybind: "ctrl+o", Action: "open_file"},
			{Label: "Open Folder", Keybind: "welcome", Action: "open_folder"},
			{Label: "Create New File", Keybind: "n", Action: "new_file"},
			{Label: "Install Package", Keybind: "ctrl+i", Action: "install_package"},
			{Label: "Close Tab", Keybind: "ctrl+w", Action: "close_tab"},
			{Label: "Next Tab", Keybind: "tab / ctrl+]", Action: "next_tab"},
			{Label: "Previous Tab", Keybind: "shift+tab", Action: "prev_tab"},
			{Label: "Explain Error", Keybind: "ctrl+e", Action: "explain_error"},
			{Label: "Get AI Hint", Keybind: "ctrl+h", Action: "ai_hint"},
			{Label: "Python Interpreter", Keybind: "ctrl+r", Action: "interpreter_picker"},
			{Label: "AI Model Picker", Keybind: "alt+m", Action: "model_picker"},
			{Label: "Theme Picker", Keybind: "alt+t", Action: "theme_picker"},
			{Label: "Quit Application", Keybind: "ctrl+q", Action: "quit"},
			{Label: "Help: Show All Commands", Keybind: "?", Action: "noop"},
		},
	}
	m.refilter()
	return m
}

func (m *commandPaletteModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *commandPaletteModel) Open(showAll bool) {
	m.query = nil
	m.index = 0
	if showAll {
		m.title = "All Commands"
	} else {
		m.title = "Command Palette"
	}
	m.refilter()
}

func (m commandPaletteModel) Update(msg tea.Msg) (commandPaletteModel, bool, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, false, nil
	}

	switch key.String() {
	case "esc", "ctrl+p":
		return m, true, nil
	case "up":
		if m.index > 0 {
			m.index--
		}
	case "down":
		if m.index < len(m.filtered)-1 {
			m.index++
		}
	case "backspace":
		if len(m.query) > 0 {
			m.query = m.query[:len(m.query)-1]
			m.refilter()
		}
	case "enter":
		if len(m.filtered) == 0 {
			return m, true, nil
		}
		action := m.filtered[m.index].command.Action
		return m, true, func() tea.Msg {
			return CommandPaletteActionMsg{Action: action}
		}
	default:
		if key.Type == tea.KeyRunes || key.Type == tea.KeySpace {
			m.query = append(m.query, []rune(key.String())...)
			m.refilter()
		}
	}

	return m, false, nil
}

func (m *commandPaletteModel) refilter() {
	query := string(m.query)
	m.filtered = m.filtered[:0]
	for _, command := range m.commands {
		indices, ok := fuzzyMatch(command.Label, query)
		if !ok {
			continue
		}
		m.filtered = append(m.filtered, commandPaletteMatch{
			command: command,
			indices: indices,
		})
	}
	if m.index >= len(m.filtered) {
		m.index = max(0, len(m.filtered)-1)
	}
}

func (m commandPaletteModel) View() string {
	boxWidth := min(max(36, int(float64(m.width)*0.6)), max(36, m.width-6))
	boxHeight := min(12, max(6, m.height-6))
	listHeight := max(1, boxHeight-4)
	start := 0
	if m.index >= listHeight {
		start = m.index - listHeight + 1
	}
	end := min(len(m.filtered), start+listHeight)

	lines := []string{
		accentStyle.Render(m.title),
		mutedStyle.Render("> " + string(m.query) + "_"),
		"",
	}
	if len(m.filtered) == 0 {
		lines = append(lines, mutedStyle.Render("No matching commands"))
	} else {
		for i := start; i < end; i++ {
			item := m.filtered[i]
			label := highlightMatchedLabel(item.command.Label, item.indices)
			keybind := mutedStyle.Render(item.command.Keybind)
			dots := strings.Repeat(".", max(1, boxWidth-lipgloss.Width(label)-lipgloss.Width(item.command.Keybind)-8))
			row := label + mutedStyle.Render(" "+dots+" ") + keybind
			style := lipgloss.NewStyle().Width(max(1, boxWidth-4))
			if i == m.index {
				style = style.Background(lipgloss.Color("12")).Foreground(lipgloss.Color("15"))
			}
			lines = append(lines, style.Render(row))
		}
	}

	return activePanelStyle.Width(boxWidth).Height(boxHeight).Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func fuzzyMatch(label, query string) (map[int]bool, bool) {
	if strings.TrimSpace(query) == "" {
		return map[int]bool{}, true
	}
	labelRunes := []rune(strings.ToLower(label))
	queryRunes := []rune(strings.ToLower(query))
	matched := make(map[int]bool)
	pos := 0
	for _, q := range queryRunes {
		found := false
		for pos < len(labelRunes) {
			if labelRunes[pos] == q {
				matched[pos] = true
				pos++
				found = true
				break
			}
			pos++
		}
		if !found {
			return nil, false
		}
	}
	return matched, true
}

func highlightMatchedLabel(label string, indices map[int]bool) string {
	if len(indices) == 0 {
		return label
	}
	var b strings.Builder
	for i, r := range []rune(label) {
		part := string(r)
		if indices[i] {
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(part))
			continue
		}
		b.WriteString(part)
	}
	return b.String()
}
