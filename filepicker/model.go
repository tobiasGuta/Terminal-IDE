package filepicker

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Mode int

const (
	PickFile Mode = iota
	PickDirectory
)

type SelectedMsg struct {
	Path string
	Mode Mode
}

type entry struct {
	name          string
	path          string
	isDir         bool
	selectCurrent bool
}

type Model struct {
	mode    Mode
	root    string
	rooted  bool
	cwd     string
	width   int
	height  int
	index   int
	entries []entry
	err     string
}

func New(root string, mode Mode) Model {
	return newModel(root, mode, false)
}

func NewRooted(root string, mode Mode) Model {
	return newModel(root, mode, true)
}

func newModel(root string, mode Mode, rooted bool) Model {
	if root == "" {
		root, _ = os.Getwd()
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	root = filepath.Clean(root)
	m := Model{cwd: root, root: root, rooted: rooted, mode: mode}
	m.reload()
	return m
}

func (m *Model) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m Model) Mode() Mode {
	return m.mode
}

func (m Model) CWD() string {
	return m.cwd
}

func (m Model) Root() string {
	return m.root
}

func (m Model) Rooted() bool {
	return m.rooted
}

func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
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
		if m.index < len(m.entries)-1 {
			m.index++
		}
	case "enter":
		if len(m.entries) == 0 {
			return m, nil
		}
		selected := m.entries[m.index]
		if selected.selectCurrent {
			return m, func() tea.Msg {
				return SelectedMsg{Path: m.cwd, Mode: m.mode}
			}
		}
		if selected.isDir {
			next := filepath.Clean(selected.path)
			if m.rooted && !isWithinRoot(m.root, next) {
				m.err = "Navigation outside the allowed root is blocked."
				return m, nil
			}
			m.cwd = next
			m.index = 0
			m.reload()
			return m, nil
		}
		if m.mode == PickFile {
			return m, func() tea.Msg {
				return SelectedMsg{Path: selected.path, Mode: m.mode}
			}
		}
	}

	return m, nil
}

func (m Model) View() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14")).Render("Open")
	mode := "file"
	if m.mode == PickDirectory {
		mode = "folder"
	}
	header := fmt.Sprintf("%s %s\n%s", title, mode, lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(m.cwd))

	available := max(3, m.height-4)
	start := 0
	if m.index >= available {
		start = m.index - available + 1
	}
	end := min(len(m.entries), start+available)

	var lines []string
	for i := start; i < end; i++ {
		item := m.entries[i]
		label := item.name
		if item.selectCurrent {
			label = "[ Select current folder ]"
		} else if item.isDir {
			label = "[DIR] " + item.name
		} else {
			label = "[FILE] " + item.name
		}

		style := lipgloss.NewStyle().PaddingLeft(1)
		if i == m.index {
			style = style.Foreground(lipgloss.Color("15")).Background(lipgloss.Color("12")).Bold(true)
		} else if item.isDir {
			style = style.Foreground(lipgloss.Color("14"))
		}
		lines = append(lines, style.Width(max(10, m.width-2)).Render(label))
	}

	if len(lines) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("No entries"))
	}
	if m.err != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render(m.err))
	}

	footer := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("esc back • enter select • ↑↓ navigate")
	return lipgloss.JoinVertical(lipgloss.Left, header, "", strings.Join(lines, "\n"), "", footer)
}

func (m *Model) reload() {
	entries, err := os.ReadDir(m.cwd)
	if err != nil {
		m.err = err.Error()
		return
	}
	m.err = ""

	var dirs []entry
	var files []entry

	if parent := filepath.Dir(m.cwd); parent != m.cwd && (!m.rooted || isWithinRoot(m.root, parent)) {
		dirs = append(dirs, entry{name: "..", path: parent, isDir: true})
	}

	if m.mode == PickDirectory {
		dirs = append(dirs, entry{name: ".", path: m.cwd, isDir: true, selectCurrent: true})
	}

	for _, item := range entries {
		entry := entry{
			name:  item.Name(),
			path:  filepath.Clean(filepath.Join(m.cwd, item.Name())),
			isDir: item.IsDir(),
		}
		if m.rooted && !isWithinRoot(m.root, entry.path) {
			continue
		}
		if entry.isDir {
			dirs = append(dirs, entry)
			continue
		}
		if m.mode == PickFile {
			files = append(files, entry)
		}
	}

	sort.Slice(dirs, func(i, j int) bool { return dirs[i].name < dirs[j].name })
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	m.entries = append(dirs, files...)
	if m.index >= len(m.entries) {
		m.index = max(0, len(m.entries)-1)
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

func isWithinRoot(root, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	if root == path {
		return true
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
