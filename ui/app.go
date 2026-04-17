package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"terminal-ide/editor"
	"terminal-ide/filepicker"
	"terminal-ide/runner"
)

type screen int

const (
	screenWelcome screen = iota
	screenPicker
	screenEditor
)

type debounceMsg struct {
	tabID int
	token int
}

type editorTab struct {
	id            int
	path          string
	editor        editor.Model
	output        outputModel
	status        string
	debounceToken int
	activeRunID   int
}

type appModel struct {
	screen       screen
	width        int
	height       int
	editorHeight int
	outputHeight int
	activeTab    int
	nextTabID    int

	welcome welcomeModel
	picker  filepicker.Model
	runner  *runner.Manager
	status  string
	focus   string
	tabs    []editorTab
}

func NewApp() tea.Model {
	return &appModel{
		screen:    screenWelcome,
		activeTab: -1,
		welcome:   newWelcomeModel(),
		picker:    filepicker.New("", filepicker.PickFile),
		runner:    runner.New(),
		status:    "Choose a file to begin.",
		focus:     "editor",
	}
}

func (m *appModel) Init() tea.Cmd {
	return waitForRunnerEvent(m.runner)
}

func (m *appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize()
		return m, nil

	case runner.StartedMsg:
		if idx := m.findTabByRunID(msg.ID); idx >= 0 {
			m.tabs[idx].output.Reset("Running...")
			m.tabs[idx].status = "Running..."
		}
		return m, waitForRunnerEvent(m.runner)

	case runner.OutputMsg:
		if idx := m.findTabByRunID(msg.ID); idx >= 0 {
			m.tabs[idx].output.Append(msg.Text, msg.IsErr)
		}
		return m, waitForRunnerEvent(m.runner)

	case runner.FinishedMsg:
		if idx := m.findTabByRunID(msg.ID); idx >= 0 {
			if idx == m.activeTab {
				m.tabs[idx].output.SetInputFocus(false)
				m.focus = "editor"
			}
			switch {
			case msg.Cancelled:
				m.tabs[idx].output.SetStatus("Run cancelled")
				m.tabs[idx].status = "Run cancelled"
			case msg.Err != nil:
				m.tabs[idx].output.SetStatus("Run finished with errors")
				m.tabs[idx].output.Append(msg.Err.Error(), true)
				m.tabs[idx].status = "Run finished with errors"
			default:
				m.tabs[idx].output.SetStatus("Run finished successfully")
				m.tabs[idx].status = "Run finished successfully"
			}
		}
		return m, waitForRunnerEvent(m.runner)

	case filepicker.SelectedMsg:
		if msg.Mode == filepicker.PickDirectory {
			m.picker = filepicker.New(msg.Path, filepicker.PickFile)
			m.picker.SetSize(m.width-6, m.height-6)
			m.screen = screenPicker
			m.status = fmt.Sprintf("Opened folder %s", msg.Path)
			return m, nil
		}

		tabIndex, err := m.openFile(msg.Path)
		if err != nil {
			m.status = err.Error()
			return m, nil
		}
		m.tabs[tabIndex].output.ClearInput()
		m.tabs[tabIndex].output.SetInputFocus(false)
		m.focus = "editor"
		return m, scheduleRun(m.bumpDebounce(tabIndex), m.tabs[tabIndex].id)

	case editor.ChangedMsg:
		if m.screen == screenEditor && m.hasActiveTab() {
			return m, scheduleRun(m.bumpDebounce(m.activeTab), m.tabs[m.activeTab].id)
		}

	case debounceMsg:
		idx := m.findTabByID(msg.tabID)
		if idx >= 0 && idx == m.activeTab && msg.token == m.tabs[idx].debounceToken && m.screen == screenEditor {
			m.startRunForTab(idx, "Queued run...")
		}
		return m, nil
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	if m.screen == screenEditor && m.hasActiveTab() && m.tabs[m.activeTab].output.InputFocused() {
		switch key.String() {
		case "ctrl+e":
			m.tabs[m.activeTab].output.SetInputFocus(false)
			m.focus = "editor"
			return m, nil
		case "ctrl+l":
			return m, nil
		default:
			if submitted, ok := m.tabs[m.activeTab].output.HandleKey(key.String()); ok {
				if err := m.runner.SendInput(m.tabs[m.activeTab].activeRunID, submitted+"\n"); err != nil {
					m.tabs[m.activeTab].output.Append(err.Error(), true)
				} else {
					m.tabs[m.activeTab].output.Append("> "+submitted, false)
				}
				return m, nil
			}
			if key.Type == tea.KeyRunes || key.Type == tea.KeySpace || key.String() == "backspace" {
				return m, nil
			}
		}
	}

	switch key.String() {
	case "ctrl+q":
		m.runner.Stop()
		return m, tea.Quit
	case "ctrl+l":
		if m.screen == screenEditor && m.hasActiveTab() {
			m.tabs[m.activeTab].output.SetInputFocus(true)
			m.focus = "output"
			return m, nil
		}
	case "ctrl+]":
		if m.screen == screenEditor && len(m.tabs) > 1 {
			m.activateTab((m.activeTab + 1) % len(m.tabs))
			return m, nil
		}
	case "shift+tab":
		if m.screen == screenEditor && len(m.tabs) > 1 {
			m.activateTab((m.activeTab - 1 + len(m.tabs)) % len(m.tabs))
			return m, nil
		}
	case "tab":
		if m.screen == screenEditor && len(m.tabs) > 1 {
			m.activateTab((m.activeTab + 1) % len(m.tabs))
			return m, nil
		}
	case "ctrl+w":
		if m.screen == screenEditor && m.hasActiveTab() {
			m.closeActiveTab()
			return m, nil
		}
	case "esc":
		if m.screen == screenEditor || m.screen == screenPicker {
			m.screen = screenWelcome
			m.status = "Returned to welcome menu."
			m.runner.Stop()
			if m.hasActiveTab() {
				m.tabs[m.activeTab].output.SetInputFocus(false)
			}
			m.focus = "editor"
			return m, nil
		}
	case "ctrl+o":
		root := pickerStartPath("")
		if m.hasActiveTab() {
			root = pickerStartPath(filepath.Dir(m.tabs[m.activeTab].path))
		}
		m.picker = filepicker.New(root, filepicker.PickFile)
		m.picker.SetSize(m.width-6, m.height-6)
		m.screen = screenPicker
		return m, nil
	case "ctrl+s":
		if m.screen == screenEditor && m.hasActiveTab() {
			tab := &m.tabs[m.activeTab]
			if err := os.WriteFile(tab.path, []byte(tab.editor.Content()), 0o644); err != nil {
				tab.status = err.Error()
			} else {
				tab.editor.MarkSaved()
				tab.status = fmt.Sprintf("Saved %s", filepath.Base(tab.path))
				m.startRunForTab(m.activeTab, "Saved and running...")
			}
		}
		return m, nil
	}

	switch m.screen {
	case screenWelcome:
		var cmd tea.Cmd
		m.welcome, cmd = m.welcome.Update(msg)
		if key.String() == "enter" {
			switch m.welcome.Selected() {
			case "Open File":
				m.picker = filepicker.New(pickerStartPath(""), filepicker.PickFile)
				m.picker.SetSize(m.width-6, m.height-6)
				m.screen = screenPicker
			case "Open Folder":
				m.picker = filepicker.New(pickerStartPath(""), filepicker.PickDirectory)
				m.picker.SetSize(m.width-6, m.height-6)
				m.screen = screenPicker
			}
		}
		return m, cmd

	case screenPicker:
		var cmd tea.Cmd
		m.picker, cmd = m.picker.Update(msg)
		return m, cmd

	case screenEditor:
		if m.hasActiveTab() {
			var cmd tea.Cmd
			m.tabs[m.activeTab].editor, cmd = m.tabs[m.activeTab].editor.Update(msg)
			return m, cmd
		}
	}

	return m, nil
}

func (m *appModel) View() string {
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	switch m.screen {
	case screenWelcome:
		return m.welcome.View()
	case screenPicker:
		card := activePanelStyle.Width(max(20, m.width-4)).Height(max(8, m.height-4)).Render(m.picker.View())
		return appPaddingStyle.Render(card)
	case screenEditor:
		if !m.hasActiveTab() {
			return m.welcome.View()
		}

		tab := m.tabs[m.activeTab]
		panelWidth := max(20, m.width-4)
		headerWidth := max(10, panelWidth-4)
		tabBar := lipgloss.NewStyle().
			Width(headerWidth).
			BorderBottom(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("8")).
			Render(m.renderTabBar(headerWidth))
		header := lipgloss.NewStyle().
			Width(headerWidth).
			BorderBottom(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("8")).
			Render(titleStyle.Render(filepath.Base(tab.path)) + "  " + mutedStyle.Render(tab.status))

		top := activePanelStyle.Width(panelWidth).Height(max(4, m.editorHeight)).Render(
			lipgloss.JoinVertical(lipgloss.Left, tabBar, header, tab.editor.View()),
		)
		bottomStyle := panelStyle.Copy().Width(panelWidth).Height(max(3, m.outputHeight))
		bottom := bottomStyle.Render(tab.output.View())
		footer := mutedStyle.Render("ctrl+s save • ctrl+o open • ctrl+w close tab • shift+tab prev • tab or ctrl+] next • ctrl+l live input")
		return appPaddingStyle.Render(lipgloss.JoinVertical(lipgloss.Left, top, bottom, footer))
	default:
		return ""
	}
}

func (m *appModel) resize() {
	usableHeight := max(8, m.height-7)
	m.editorHeight = int(float64(usableHeight) * 0.6)
	if m.editorHeight < 4 {
		m.editorHeight = 4
	}
	m.outputHeight = usableHeight - m.editorHeight
	if m.outputHeight < 3 {
		m.outputHeight = 3
	}

	m.welcome.SetSize(m.width, m.height)
	m.picker.SetSize(max(10, m.width-6), max(6, m.height-6))
	for i := range m.tabs {
		m.tabs[i].editor.SetSize(max(10, m.width-8), max(3, m.editorHeight-2))
		m.tabs[i].output.SetSize(max(10, m.width-8), max(2, m.outputHeight-2))
	}
}

func (m *appModel) openFile(path string) (int, error) {
	path = pickerStartPath(path)
	for i := range m.tabs {
		if m.tabs[i].path == path {
			m.activateTab(i)
			return i, nil
		}
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return -1, err
	}

	tab := editorTab{
		id:     m.nextTabID,
		path:   path,
		editor: editor.New(),
		output: newOutputModel(),
		status: fmt.Sprintf("Editing %s", filepath.Base(path)),
	}
	m.nextTabID++
	tab.editor.LoadFile(path, string(content))
	tab.editor.SetSize(max(10, m.width-8), max(3, m.editorHeight-2))
	tab.output.SetSize(max(10, m.width-8), max(2, m.outputHeight-2))

	m.tabs = append(m.tabs, tab)
	m.activateTab(len(m.tabs) - 1)
	return m.activeTab, nil
}

func (m *appModel) activateTab(index int) {
	if index < 0 || index >= len(m.tabs) {
		return
	}
	if m.hasActiveTab() {
		m.tabs[m.activeTab].output.SetInputFocus(false)
	}
	m.activeTab = index
	m.screen = screenEditor
	m.focus = "editor"
	m.resize()
}

func (m *appModel) closeActiveTab() {
	if !m.hasActiveTab() {
		return
	}

	if m.tabs[m.activeTab].activeRunID != 0 {
		m.runner.Stop()
	}

	m.tabs = append(m.tabs[:m.activeTab], m.tabs[m.activeTab+1:]...)
	if len(m.tabs) == 0 {
		m.activeTab = -1
		m.screen = screenWelcome
		m.status = "All tabs closed."
		return
	}

	if m.activeTab >= len(m.tabs) {
		m.activeTab = len(m.tabs) - 1
	}
	m.activateTab(m.activeTab)
}

func (m *appModel) startRunForTab(index int, status string) {
	if index < 0 || index >= len(m.tabs) {
		return
	}
	tab := &m.tabs[index]
	tab.activeRunID = m.runner.Start(tab.path, tab.editor.Content())
	tab.output.Reset(status)
	tab.output.ClearInput()
	tab.output.SetInputFocus(false)
	tab.status = status
}

func (m *appModel) bumpDebounce(index int) int {
	if index < 0 || index >= len(m.tabs) {
		return 0
	}
	m.tabs[index].debounceToken++
	return m.tabs[index].debounceToken
}

func (m *appModel) renderTabBar(width int) string {
	if len(m.tabs) == 0 {
		return ""
	}

	var rendered []string
	for i, tab := range m.tabs {
		label := filepath.Base(tab.path)
		if tab.editor.Dirty() {
			label += " *"
		}
		label = truncateLabel(label, 20)
		style := lipgloss.NewStyle().
			Padding(0, 1).
			MarginRight(1).
			Foreground(lipgloss.Color("252")).
			Background(lipgloss.Color("236"))
		if i == m.activeTab {
			style = style.Background(lipgloss.Color("12")).Foreground(lipgloss.Color("15")).Bold(true)
		}
		rendered = append(rendered, style.Render(label))
	}

	bar := lipgloss.JoinHorizontal(lipgloss.Left, rendered...)
	if lipgloss.Width(bar) > width {
		bar = lipgloss.NewStyle().MaxWidth(width).Render(bar)
	}
	return bar
}

func truncateLabel(label string, maxWidth int) string {
	runes := []rune(label)
	if len(runes) <= maxWidth {
		return label
	}
	if maxWidth <= 1 {
		return string(runes[:maxWidth])
	}
	return string(runes[:maxWidth-1]) + "…"
}

func (m *appModel) hasActiveTab() bool {
	return m.activeTab >= 0 && m.activeTab < len(m.tabs)
}

func (m *appModel) findTabByRunID(runID int) int {
	for i := range m.tabs {
		if m.tabs[i].activeRunID == runID {
			return i
		}
	}
	return -1
}

func (m *appModel) findTabByID(tabID int) int {
	for i := range m.tabs {
		if m.tabs[i].id == tabID {
			return i
		}
	}
	return -1
}

func scheduleRun(token, tabID int) tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
		return debounceMsg{tabID: tabID, token: token}
	})
}

func waitForRunnerEvent(manager *runner.Manager) tea.Cmd {
	return func() tea.Msg {
		return <-manager.Events()
	}
}

func pickerStartPath(path string) string {
	if path == "" {
		if wd, err := os.Getwd(); err == nil {
			path = wd
		} else {
			path = "."
		}
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
