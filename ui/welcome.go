package ui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const welcomeASCIIArt = `████████╗███████╗██████╗ ███╗   ███╗██╗███╗   ██╗ █████╗ ██╗         ██╗██████╗ ███████╗
╚══██╔══╝██╔════╝██╔══██╗████╗ ████║██║████╗  ██║██╔══██╗██║        ██╔╝██╔══██╗██╔════╝
   ██║   █████╗  ██████╔╝██╔████╔██║██║██╔██╗ ██║███████║██║       ██╔╝ ██║  ██║█████╗
   ██║   ██╔══╝  ██╔══██╗██║╚██╔╝██║██║██║╚██╗██║██╔══██║██║      ██╔╝  ██║  ██║██╔══╝
   ██║   ███████╗██║  ██║██║ ╚═╝ ██║██║██║ ╚████║██║  ██║███████╗██╔╝   ██████╔╝███████╗
   ╚═╝   ╚══════╝╚═╝  ╚═╝╚═╝     ╚═╝╚═╝╚═╝  ╚═══╝╚═╝  ╚═╝╚══════╝╚═╝    ╚═════╝ ╚══════╝
─────────────────────── go · python · ai-powered ───────────────────────`

type welcomeModel struct {
	index         int
	options       []string
	width         int
	height        int
	message       string
	cursorVisible bool
}

type cursorTickMsg struct{}

func newWelcomeModel() welcomeModel {
	return welcomeModel{
		options:       []string{"Open File", "Open Folder", "Create New File", "Recent Files (Soon)"},
		cursorVisible: true,
	}
}

func (m welcomeModel) Init() tea.Cmd {
	return scheduleCursorTick()
}

func (m *welcomeModel) SetSize(width, height int) {
	m.width = width
	m.height = height
}

func (m welcomeModel) Selected() string {
	return m.options[m.index]
}

func (m *welcomeModel) SetMessage(message string) {
	m.message = message
}

func (m welcomeModel) Update(msg tea.Msg) (welcomeModel, tea.Cmd) {
	switch msg.(type) {
	case cursorTickMsg:
		m.cursorVisible = !m.cursorVisible
		return m, scheduleCursorTick()
	}

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
		if m.index < len(m.options)-1 {
			m.index++
		}
	}

	return m, nil
}

func (m welcomeModel) View() string {
	var items []string
	for i, option := range m.options {
		style := lipgloss.NewStyle().Padding(0, 1).Width(24)
		indicator := "   "
		if i == m.index {
			style = style.Foreground(lipgloss.Color("15")).Bold(true)
			if m.cursorVisible {
				indicator = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(">_\u2588")
			} else {
				indicator = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(">_ ")
			}
		} else if strings.Contains(option, "Soon") {
			style = style.Foreground(lipgloss.Color("8"))
		} else {
			style = style.Foreground(lipgloss.Color("14"))
		}
		items = append(items, lipgloss.JoinHorizontal(lipgloss.Left, style.Render(option), " ", indicator))
	}

	logo := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14")).Render(welcomeASCIIArt)
	body := lipgloss.JoinVertical(lipgloss.Center,
		logo,
		"",
		strings.Join(items, "\n"),
		"",
		errorStyle.Render(m.message),
		mutedStyle.Render(""),
		mutedStyle.Render("↑/↓ move • enter select • ctrl+q quit"),
	)

	card := activePanelStyle.Padding(1, 3).Render(body)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, card)
}

func scheduleCursorTick() tea.Cmd {
	return tea.Tick(530*time.Millisecond, func(time.Time) tea.Msg {
		return cursorTickMsg{}
	})
}
