package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type newFileModel struct {
	baseDir     string
	name        []rune
	extension   []rune
	activeField int
	width       int
	height      int
	message     string
}

func newNewFileModel(baseDir string) newFileModel {
	return newFileModel{
		baseDir:     baseDir,
		extension:   []rune("py"),
		activeField: 0,
	}
}

func (m *newFileModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *newFileModel) SetBaseDir(baseDir string) {
	m.baseDir = baseDir
}

func (m *newFileModel) SetMessage(message string) {
	m.message = message
}

func (m newFileModel) FilePath() string {
	ext := strings.TrimPrefix(strings.TrimSpace(string(m.extension)), ".")
	name := strings.TrimSpace(string(m.name))
	if ext == "" {
		return filepath.Join(m.baseDir, name)
	}
	return filepath.Join(m.baseDir, fmt.Sprintf("%s.%s", name, ext))
}

func (m newFileModel) ValidatedPath() (string, error) {
	name := strings.TrimSpace(string(m.name))
	if name == "" {
		return "", fmt.Errorf("enter a file name")
	}
	if strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("file name cannot include path separators")
	}

	ext := strings.TrimPrefix(strings.TrimSpace(string(m.extension)), ".")
	if ext == "" {
		return "", fmt.Errorf("enter a file extension")
	}
	if strings.ContainsAny(ext, `/\ `) {
		return "", fmt.Errorf("extension cannot include spaces or path separators")
	}

	return filepath.Join(m.baseDir, name+"."+ext), nil
}

func (m newFileModel) Update(msg tea.Msg) (newFileModel, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch key.String() {
	case "up", "shift+tab":
		m.activeField = (m.activeField + 1) % 2
	case "down", "tab":
		m.activeField = (m.activeField + 1) % 2
	case "backspace":
		if m.activeField == 0 && len(m.name) > 0 {
			m.name = m.name[:len(m.name)-1]
		}
		if m.activeField == 1 && len(m.extension) > 0 {
			m.extension = m.extension[:len(m.extension)-1]
		}
	default:
		if key.Type == tea.KeyRunes || key.Type == tea.KeySpace {
			if m.activeField == 0 {
				m.name = append(m.name, []rune(key.String())...)
			} else {
				m.extension = append(m.extension, []rune(key.String())...)
			}
		}
	}

	return m, nil
}

func (m newFileModel) View() string {
	field := func(label string, value []rune, active bool) string {
		style := lipgloss.NewStyle().
			Width(28).
			Padding(0, 1).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("8"))
		if active {
			style = style.BorderForeground(lipgloss.Color("12"))
		}
		text := string(value)
		if active {
			text += "_"
		}
		return lipgloss.JoinVertical(lipgloss.Left,
			mutedStyle.Render(label),
			style.Render(text),
		)
	}

	body := lipgloss.JoinVertical(lipgloss.Left,
		titleStyle.Render("Create New File"),
		mutedStyle.Render(m.baseDir),
		"",
		field("File name", m.name, m.activeField == 0),
		"",
		field("Extension", m.extension, m.activeField == 1),
		"",
		mutedStyle.Render("Will create: "+m.FilePath()),
		errorStyle.Render(m.message),
		mutedStyle.Render("tab switch fields • enter create • esc back"),
	)

	card := activePanelStyle.Padding(1, 2).Render(body)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card)
}
