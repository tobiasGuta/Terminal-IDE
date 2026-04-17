package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"terminal-ide/ai"
	"terminal-ide/editor"
	"terminal-ide/filepicker"
	"terminal-ide/runner"
)

type screen int

const (
	screenWelcome screen = iota
	screenPicker
	screenNewFile
	screenInterpreterPicker
	screenModelPicker
	screenEditor
)

type debounceMsg struct {
	tabID int
	token int
}

type aiResultMsg struct {
	tabIndex    int
	content     string
	header      string
	headerColor string
}

type aiResponseMsg struct {
	tabIndex    int
	text        string
	err         error
	header      string
	headerColor string
}

type aiThinkingTickMsg struct{}

type editorTab struct {
	id                int
	path              string
	editor            editor.Model
	output            outputModel
	status            string
	debounceToken     int
	activeRunID       int
	pythonInterpreter string
	aiCooldownUntil   time.Time
	hintLevel         int
	lastHintSource    string
}

type tabHitbox struct {
	index int
	start int
	end   int
}

type editorLayout struct {
	panelX       int
	panelY       int
	panelWidth   int
	topHeight    int
	bottomY      int
	bottomHeight int
	tabBarY      int
	tabBarX      int
	editorBodyY  int
	contentX     int
}

type appModel struct {
	screen       screen
	prevScreen   screen
	screenStack  []screen
	width        int
	height       int
	editorHeight int
	outputHeight int
	activeTab    int
	nextTabID    int

	welcome            welcomeModel
	picker             filepicker.Model
	newFile            newFileModel
	interpreterPicker  interpreterPickerModel
	modelPicker        modelPickerModel
	runner             *runner.Manager
	aiClient           *ai.Client
	selectedAIModel    string
	aiLoading          bool
	aiThinkingFrame    int
	aiThinkingTab      int
	status             string
	focus              string
	tabs               []editorTab
	layout             editorLayout
	pythonInterpreters []runner.PythonInterpreter
}

func NewApp() tea.Model {
	interpreters, _ := runner.DiscoverPythonInterpreters()
	aiClient := ai.NewClient(os.Getenv("OPENAI_API_KEY"), os.Getenv("GEMINI_API_KEY"), "")
	return &appModel{
		screen:             screenWelcome,
		prevScreen:         screenWelcome,
		activeTab:          -1,
		welcome:            newWelcomeModel(),
		picker:             filepicker.New("", filepicker.PickFile),
		newFile:            newNewFileModel(pickerStartPath("")),
		interpreterPicker:  newInterpreterPickerModel(nil, ""),
		modelPicker:        newModelPickerModel(nil, ""),
		runner:             runner.New(),
		aiClient:           aiClient,
		selectedAIModel:    aiClient.Model(),
		aiThinkingTab:      -1,
		status:             "Choose a file to begin.",
		focus:              "editor",
		pythonInterpreters: interpreters,
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

	case runner.ExecutionMsg:
		if idx := m.findTabByRunID(msg.ID); idx >= 0 {
			m.tabs[idx].editor.SetExecution(msg.Line, msg.Waiting)
			switch msg.State {
			case "waiting_input":
				m.tabs[idx].status = fmt.Sprintf("Waiting for input on line %d", msg.Line)
			case "line", "resumed":
				m.tabs[idx].status = fmt.Sprintf("Executing line %d", msg.Line)
			case "finished":
				m.tabs[idx].status = fmt.Sprintf("Finished on line %d", msg.Line)
			case "exception":
				m.tabs[idx].status = fmt.Sprintf("Stopped on line %d", msg.Line)
			}
		}
		return m, waitForRunnerEvent(m.runner)

	case runner.FinishedMsg:
		if idx := m.findTabByRunID(msg.ID); idx >= 0 {
			m.tabs[idx].activeRunID = 0
			if idx == m.activeTab {
				m.tabs[idx].output.SetInputFocus(false)
				m.focus = "editor"
			}
			finalLine := m.tabs[idx].editor.ExecutionLine()
			switch {
			case msg.Cancelled:
				m.tabs[idx].editor.ClearExecution()
				m.tabs[idx].output.SetStatus("Run cancelled")
				m.tabs[idx].status = "Run cancelled"
			case msg.Err != nil:
				m.tabs[idx].output.SetStatus("Run finished with errors")
				m.tabs[idx].output.Append(msg.Err.Error(), true)
				if finalLine > 0 {
					m.tabs[idx].status = fmt.Sprintf("Run stopped on line %d", finalLine)
				} else {
					m.tabs[idx].status = "Run finished with errors"
				}
			default:
				m.tabs[idx].output.SetStatus("Run finished successfully")
				if finalLine > 0 {
					m.tabs[idx].status = fmt.Sprintf("Run finished on line %d", finalLine)
				} else {
					m.tabs[idx].status = "Run finished successfully"
				}
			}
		}
		return m, waitForRunnerEvent(m.runner)

	case aiResultMsg:
		if msg.tabIndex >= 0 && msg.tabIndex < len(m.tabs) && msg.content != "" {
			m.tabs[msg.tabIndex].output.AppendAIBlock(msg.header, msg.headerColor, msg.content)
			m.tabs[msg.tabIndex].status = "AI response ready"
		}
		return m, nil

	case aiResponseMsg:
		m.aiLoading = false
		m.aiThinkingFrame = 0
		m.aiThinkingTab = -1
		if msg.tabIndex >= 0 && msg.tabIndex < len(m.tabs) {
			content := strings.TrimSpace(msg.text)
			if msg.err != nil {
				content = formatAIError(msg.err)
			}
			if content != "" {
				m.tabs[msg.tabIndex].output.AppendAIBlock(msg.header, msg.headerColor, content)
				m.tabs[msg.tabIndex].status = "AI response ready"
			}
		}
		return m, nil

	case aiThinkingTickMsg:
		if !m.aiLoading {
			return m, nil
		}
		m.aiThinkingFrame = (m.aiThinkingFrame + 1) % len(aiThinkingFrames)
		return m, scheduleAIThinkingTick()

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
			if m.tabs[m.activeTab].activeRunID != 0 {
				m.runner.StopRun(m.tabs[m.activeTab].activeRunID)
				m.tabs[m.activeTab].activeRunID = 0
				m.tabs[m.activeTab].output.Reset("Queued run...")
				m.tabs[m.activeTab].output.ClearInput()
				m.tabs[m.activeTab].output.SetInputFocus(false)
				m.tabs[m.activeTab].status = "Queued run..."
				m.focus = "editor"
			}
			m.tabs[m.activeTab].hintLevel = 1
			m.tabs[m.activeTab].lastHintSource = ""
			return m, scheduleRun(m.bumpDebounce(m.activeTab), m.tabs[m.activeTab].id)
		}

	case debounceMsg:
		idx := m.findTabByID(msg.tabID)
		if idx >= 0 && idx == m.activeTab && msg.token == m.tabs[idx].debounceToken && m.screen == screenEditor {
			m.startRunForTab(idx, "Queued run...")
		}
		return m, nil

	case tea.MouseMsg:
		if m.screen == screenEditor {
			switch {
			case msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft:
				return m, m.handleMouseClick(msg.X, msg.Y)
			case msg.Action == tea.MouseActionMotion && msg.Button == tea.MouseButtonLeft:
				m.handleMouseDrag(msg.X, msg.Y)
				return m, nil
			case msg.Action == tea.MouseActionRelease && msg.Button == tea.MouseButtonLeft:
				m.handleMouseRelease()
				return m, nil
			case msg.Button == tea.MouseButtonWheelUp:
				m.handleMouseScroll(msg.X, msg.Y, -3)
				return m, nil
			case msg.Button == tea.MouseButtonWheelDown:
				m.handleMouseScroll(msg.X, msg.Y, 3)
				return m, nil
			}
		}
		return m, nil
	}

	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	if key.Paste && m.screen == screenEditor && m.hasActiveTab() {
		text := string(key.Runes)
		if m.tabs[m.activeTab].output.InputFocused() {
			m.tabs[m.activeTab].output.PasteInput(text)
			m.tabs[m.activeTab].status = "Pasted into live input"
			return m, nil
		}
		m.tabs[m.activeTab].editor.PasteText(text)
		m.tabs[m.activeTab].status = "Pasted into editor"
		return m, scheduleRun(m.bumpDebounce(m.activeTab), m.tabs[m.activeTab].id)
	}

	if m.screen == screenEditor && m.hasActiveTab() && m.tabs[m.activeTab].output.InputFocused() {
		switch key.String() {
		case "ctrl+e":
			m.tabs[m.activeTab].output.SetInputFocus(false)
			m.focus = "editor"
			return m, nil
		case "ctrl+v":
			text, err := readClipboard()
			if err != nil {
				m.tabs[m.activeTab].status = err.Error()
			} else {
				m.tabs[m.activeTab].output.PasteInput(text)
				m.tabs[m.activeTab].status = "Pasted into live input"
			}
			return m, nil
		case "ctrl+c":
			text := m.tabs[m.activeTab].output.InputValue()
			status := "Copied input to clipboard"
			if m.tabs[m.activeTab].output.HasSelection() {
				text = m.tabs[m.activeTab].output.SelectedText()
				status = "Copied selected output"
			}
			if err := writeClipboard(text); err != nil {
				m.tabs[m.activeTab].status = err.Error()
			} else {
				m.tabs[m.activeTab].status = status
			}
			return m, nil
		case "ctrl+l":
			return m, nil
		default:
			if submitted, ok := m.tabs[m.activeTab].output.HandleKey(key.String()); ok {
				if err := m.runner.SendInput(m.tabs[m.activeTab].activeRunID, submitted+"\n"); err != nil {
					m.tabs[m.activeTab].output.Append(err.Error(), true)
				} else {
					m.tabs[m.activeTab].output.EchoSubmittedInput(submitted)
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
	case "ctrl+e":
		if m.screen == screenEditor && m.hasActiveTab() && !m.tabs[m.activeTab].output.InputFocused() {
			cmd := m.startAIErrorExplanation(m.activeTab)
			return m, cmd
		}
	case "ctrl+h":
		if m.screen == screenEditor && m.hasActiveTab() && !m.tabs[m.activeTab].output.InputFocused() {
			cmd := m.startAIHint(m.activeTab)
			return m, cmd
		}
	case "ctrl+m", "alt+m":
		if m.screen == screenEditor {
			if handled := m.openModelPicker(); handled {
				return m, nil
			}
		}
	case "ctrl+c":
		if m.screen == screenEditor && m.hasActiveTab() {
			var text string
			switch m.focus {
			case "output":
				if m.tabs[m.activeTab].output.HasSelection() {
					text = m.tabs[m.activeTab].output.SelectedText()
				} else if m.tabs[m.activeTab].output.InputFocused() {
					text = m.tabs[m.activeTab].output.InputValue()
				}
			default:
				if m.tabs[m.activeTab].editor.HasSelection() {
					text = m.tabs[m.activeTab].editor.SelectedText()
				} else {
					text = m.tabs[m.activeTab].editor.CurrentLine()
				}
			}
			if text == "" && m.tabs[m.activeTab].output.HasSelection() && m.focus != "editor" {
				text = m.tabs[m.activeTab].output.SelectedText()
			}
			if err := writeClipboard(text); err != nil {
				m.tabs[m.activeTab].status = err.Error()
			} else {
				m.tabs[m.activeTab].status = "Copied to clipboard"
			}
			return m, nil
		}
	case "ctrl+v":
		if m.screen == screenEditor && m.hasActiveTab() {
			text, err := readClipboard()
			if err != nil {
				m.tabs[m.activeTab].status = err.Error()
				return m, nil
			}
			if m.tabs[m.activeTab].output.InputFocused() {
				m.tabs[m.activeTab].output.PasteInput(text)
				m.tabs[m.activeTab].status = "Pasted into live input"
			} else {
				m.tabs[m.activeTab].editor.PasteText(text)
				m.tabs[m.activeTab].status = "Pasted into editor"
				return m, scheduleRun(m.bumpDebounce(m.activeTab), m.tabs[m.activeTab].id)
			}
			return m, nil
		}
	case "ctrl+l":
		if m.screen == screenEditor && m.hasActiveTab() {
			m.tabs[m.activeTab].output.SetInputFocus(true)
			m.focus = "output"
			return m, nil
		}
	case "ctrl+r":
		if m.screen == screenEditor && m.hasActiveTab() && strings.EqualFold(filepath.Ext(m.tabs[m.activeTab].path), ".py") {
			m.interpreterPicker = newInterpreterPickerModel(m.pythonInterpreters, m.tabs[m.activeTab].pythonInterpreter)
			m.interpreterPicker.SetSize(m.width, m.height)
			m.pushScreen(m.screen)
			m.screen = screenInterpreterPicker
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
		switch m.screen {
		case screenPicker, screenNewFile, screenInterpreterPicker, screenModelPicker:
			m.screen = m.popScreen()
			if m.screen == screenEditor && m.hasActiveTab() {
				m.tabs[m.activeTab].output.SetInputFocus(false)
				m.focus = "editor"
				m.status = "Cancelled."
			} else {
				m.status = "Returned to welcome menu."
			}
			return m, nil
		case screenEditor:
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
		m.pushScreen(m.screen)
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
				m.welcome.SetMessage("")
				m.picker = filepicker.New(pickerStartPath(""), filepicker.PickFile)
				m.picker.SetSize(m.width-6, m.height-6)
				m.pushScreen(screenWelcome)
				m.screen = screenPicker
			case "Open Folder":
				m.welcome.SetMessage("")
				m.picker = filepicker.New(pickerStartPath(""), filepicker.PickDirectory)
				m.picker.SetSize(m.width-6, m.height-6)
				m.pushScreen(screenWelcome)
				m.screen = screenPicker
			case "Create New File":
				m.welcome.SetMessage("")
				m.newFile = newNewFileModel(m.defaultCreateDir())
				m.newFile.SetSize(m.width, m.height)
				m.pushScreen(screenWelcome)
				m.screen = screenNewFile
			case "Recent Files (Soon)":
				m.welcome.SetMessage("Recent Files is not implemented yet.")
			}
		}
		return m, cmd

	case screenPicker:
		var cmd tea.Cmd
		m.picker, cmd = m.picker.Update(msg)
		return m, cmd

	case screenNewFile:
		var cmd tea.Cmd
		m.newFile, cmd = m.newFile.Update(msg)
		if key.String() == "enter" {
			path, err := m.newFile.ValidatedPath()
			if err != nil {
				m.newFile.SetMessage(err.Error())
				return m, nil
			}
			if _, err := os.Stat(path); err == nil {
				m.newFile.SetMessage("file already exists")
				return m, nil
			}
			if err := os.WriteFile(path, []byte{}, 0o644); err != nil {
				m.newFile.SetMessage(err.Error())
				return m, nil
			}
			tabIndex, err := m.openFile(path)
			if err != nil {
				m.newFile.SetMessage(err.Error())
				return m, nil
			}
			m.tabs[tabIndex].status = fmt.Sprintf("Created %s", filepath.Base(path))
			m.screen = screenEditor
		}
		return m, cmd

	case screenInterpreterPicker:
		var cmd tea.Cmd
		m.interpreterPicker, cmd = m.interpreterPicker.Update(msg)
		if key.String() == "enter" && m.hasActiveTab() {
			selected := m.interpreterPicker.SelectedCommand()
			m.tabs[m.activeTab].pythonInterpreter = selected
			if selected == "" {
				m.tabs[m.activeTab].status = "Interpreter: auto"
			} else {
				label := filepath.Base(selected)
				for _, item := range m.pythonInterpreters {
					if item.Path == selected {
						label = item.Command
						break
					}
				}
				m.tabs[m.activeTab].status = fmt.Sprintf("Interpreter: %s", label)
			}
			m.screen = m.popScreen()
			m.startRunForTab(m.activeTab, "Interpreter changed, running...")
			return m, nil
		}
		return m, cmd

	case screenModelPicker:
		var cmd tea.Cmd
		m.modelPicker, cmd = m.modelPicker.Update(msg)
		if key.String() == "enter" {
			provider, model := m.modelPicker.Selected()
			if provider != "" && model != "" {
				m.aiClient.SetModel(provider, model)
				m.selectedAIModel = m.aiClient.Model()
				m.status = fmt.Sprintf("AI model set to %s", m.selectedAIModel)
			}
			m.screen = m.popScreen()
			return m, nil
		}
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
	case screenNewFile:
		return m.newFile.View()
	case screenInterpreterPicker:
		return m.interpreterPicker.View()
	case screenModelPicker:
		return m.modelPicker.View()
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
			Render(titleStyle.Render(filepath.Base(tab.path)) + "  " + mutedStyle.Render(tab.status) + m.renderInterpreterBadge(tab))

		m.layout = editorLayout{
			panelX:       appPaddingStyle.GetHorizontalFrameSize() / 2,
			panelY:       appPaddingStyle.GetVerticalFrameSize() / 2,
			panelWidth:   panelWidth,
			topHeight:    max(4, m.editorHeight) + activePanelStyle.GetVerticalFrameSize(),
			bottomHeight: max(3, m.outputHeight) + panelStyle.GetVerticalFrameSize(),
			tabBarY:      appPaddingStyle.GetVerticalFrameSize() / 2,
			tabBarX:      appPaddingStyle.GetHorizontalFrameSize()/2 + activePanelStyle.GetHorizontalFrameSize()/2,
		}
		m.layout.bottomY = m.layout.panelY + m.layout.topHeight
		m.layout.editorBodyY = m.layout.panelY + lipgloss.Height(tabBar) + lipgloss.Height(header) + activePanelStyle.GetVerticalFrameSize()/2 - 1
		m.layout.contentX = m.layout.tabBarX

		top := activePanelStyle.Width(panelWidth).Height(max(4, m.editorHeight)).Render(
			lipgloss.JoinVertical(lipgloss.Left, tabBar, header, tab.editor.View()),
		)
		bottomStyle := panelStyle.Copy().Width(panelWidth).Height(max(3, m.outputHeight))
		bottom := bottomStyle.Render(tab.output.View())
		footerLeft := "ctrl+s save • ctrl+o open • ctrl+w close tab • ctrl+e explain • ctrl+h hint • alt+m model • shift+tab prev • tab or ctrl+] next • ctrl+r interpreter • ctrl+c/cv clipboard • mouse focus"
		footer := renderFooter(panelWidth, footerLeft, m.selectedAIModel, m.aiStatusText())
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
	m.newFile.SetSize(m.width, m.height)
	m.interpreterPicker.SetSize(m.width, m.height)
	m.modelPicker.SetSize(m.width, m.height)
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
		m.runner.StopRun(m.tabs[m.activeTab].activeRunID)
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
	opts := runner.RunOptions{}
	if strings.EqualFold(filepath.Ext(tab.path), ".py") {
		opts.PythonInterpreter = tab.pythonInterpreter
	}
	tab.activeRunID = m.runner.Start(tab.path, tab.editor.Content(), opts)
	tab.editor.ClearExecution()
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
	for i, item := range m.tabItems() {
		style := lipgloss.NewStyle().
			Padding(0, 1).
			MarginRight(1).
			Foreground(lipgloss.Color("252")).
			Background(lipgloss.Color("236"))
		if i == m.activeTab {
			style = style.Background(lipgloss.Color("12")).Foreground(lipgloss.Color("15")).Bold(true)
		}
		rendered = append(rendered, style.Render(item.label))
	}

	bar := lipgloss.JoinHorizontal(lipgloss.Left, rendered...)
	if lipgloss.Width(bar) > width {
		bar = lipgloss.NewStyle().MaxWidth(width).Render(bar)
	}
	return bar
}

func (m *appModel) tabItems() []struct {
	label string
	index int
} {
	items := make([]struct {
		label string
		index int
	}, 0, len(m.tabs))
	for i, tab := range m.tabs {
		label := filepath.Base(tab.path)
		if tab.editor.Dirty() {
			label += " *"
		}
		items = append(items, struct {
			label string
			index int
		}{
			label: truncateLabel(label, 20),
			index: i,
		})
	}
	return items
}

func (m *appModel) tabHitboxes() []tabHitbox {
	items := m.tabItems()
	hitboxes := make([]tabHitbox, 0, len(items))
	offset := 0
	for _, item := range items {
		// Padding(0,1) + MarginRight(1)
		width := lipgloss.Width(item.label) + 3
		hitboxes = append(hitboxes, tabHitbox{
			index: item.index,
			start: offset,
			end:   offset + width,
		})
		offset += width
	}
	return hitboxes
}

func (m *appModel) handleMouseClick(x, y int) tea.Cmd {
	if !m.hasActiveTab() {
		return nil
	}

	layout := m.currentEditorLayout()

	if y >= layout.panelY && y < layout.panelY+layout.topHeight && x >= layout.panelX && x < layout.panelX+layout.panelWidth {
		if y >= layout.tabBarY && y < layout.tabBarY+2 {
			relativeX := x - layout.tabBarX
			for _, hitbox := range m.tabHitboxes() {
				if (relativeX >= hitbox.start && relativeX < hitbox.end) ||
					(relativeX-1 >= hitbox.start && relativeX-1 < hitbox.end) {
					m.activateTab(hitbox.index)
					return nil
				}
			}
		}

		m.tabs[m.activeTab].output.SetInputFocus(false)
		m.focus = "editor"

		if y >= layout.editorBodyY {
			m.tabs[m.activeTab].editor.BeginSelectionFromView(y-layout.editorBodyY, x-layout.contentX)
		}
		return nil
	}

	if y >= layout.bottomY && y < layout.bottomY+layout.bottomHeight && x >= layout.panelX && x < layout.panelX+layout.panelWidth {
		viewRow := y - (layout.bottomY + 1)
		col := x - layout.contentX
		outputRows := m.tabs[m.activeTab].output.visibleOutputLineCount()
		inputRow := m.tabs[m.activeTab].output.inputViewRow()
		m.tabs[m.activeTab].output.SetInputFocus(viewRow == inputRow)
		m.focus = "output"
		if viewRow >= 1 && viewRow <= outputRows {
			m.tabs[m.activeTab].output.BeginSelection(viewRow-1, col)
		} else {
			m.tabs[m.activeTab].output.ClearSelection()
		}
		return nil
	}

	return nil
}

func (m *appModel) handleMouseDrag(x, y int) {
	if !m.hasActiveTab() {
		return
	}
	layout := m.currentEditorLayout()
	if y >= layout.panelY && y < layout.panelY+layout.topHeight && x >= layout.panelX && x < layout.panelX+layout.panelWidth {
		if y >= layout.editorBodyY {
			m.tabs[m.activeTab].editor.UpdateSelectionFromView(y-layout.editorBodyY, x-layout.contentX)
		}
		return
	}
	if y >= layout.bottomY && y < layout.bottomY+layout.bottomHeight && x >= layout.panelX && x < layout.panelX+layout.panelWidth {
		viewRow := y - (layout.bottomY + 1)
		col := x - layout.contentX
		if viewRow >= 1 {
			m.tabs[m.activeTab].output.UpdateSelection(viewRow-1, col)
		}
	}
}

func (m *appModel) handleMouseRelease() {
	if !m.hasActiveTab() {
		return
	}
	m.tabs[m.activeTab].editor.EndSelection()
	m.tabs[m.activeTab].output.EndSelection()
}

func (m *appModel) renderInterpreterBadge(tab editorTab) string {
	if !strings.EqualFold(filepath.Ext(tab.path), ".py") {
		return ""
	}

	label := "auto"
	if tab.pythonInterpreter != "" {
		label = filepath.Base(tab.pythonInterpreter)
		for _, item := range m.pythonInterpreters {
			if item.Path == tab.pythonInterpreter {
				label = item.Command
				break
			}
		}
	}
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	return "  " + style.Render("[py: "+label+"]")
}

func (m *appModel) handleMouseScroll(x, y, delta int) {
	if !m.hasActiveTab() {
		return
	}

	layout := m.currentEditorLayout()
	if y >= layout.panelY && y < layout.panelY+layout.topHeight && x >= layout.panelX && x < layout.panelX+layout.panelWidth {
		m.tabs[m.activeTab].editor.Scroll(delta)
	}
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

func (m *appModel) currentEditorLayout() editorLayout {
	if m.layout.panelWidth != 0 {
		return m.layout
	}
	return editorLayout{
		panelX:       appPaddingStyle.GetHorizontalFrameSize() / 2,
		panelY:       appPaddingStyle.GetVerticalFrameSize() / 2,
		panelWidth:   max(20, m.width-4),
		topHeight:    max(4, m.editorHeight) + activePanelStyle.GetVerticalFrameSize(),
		bottomY:      appPaddingStyle.GetVerticalFrameSize()/2 + max(4, m.editorHeight) + activePanelStyle.GetVerticalFrameSize(),
		bottomHeight: max(3, m.outputHeight) + panelStyle.GetVerticalFrameSize(),
		tabBarY:      appPaddingStyle.GetVerticalFrameSize() / 2,
		tabBarX:      appPaddingStyle.GetHorizontalFrameSize()/2 + activePanelStyle.GetHorizontalFrameSize()/2,
		editorBodyY:  appPaddingStyle.GetVerticalFrameSize()/2 + 3,
		contentX:     appPaddingStyle.GetHorizontalFrameSize()/2 + activePanelStyle.GetHorizontalFrameSize()/2,
	}
}

func (m *appModel) pushScreen(s screen) {
	m.prevScreen = s
	m.screenStack = append(m.screenStack, s)
}

func (m *appModel) popScreen() screen {
	if len(m.screenStack) == 0 {
		m.prevScreen = screenWelcome
		return screenWelcome
	}
	last := m.screenStack[len(m.screenStack)-1]
	m.screenStack = m.screenStack[:len(m.screenStack)-1]
	m.prevScreen = last
	return last
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

func scheduleAIThinkingTick() tea.Cmd {
	return tea.Tick(180*time.Millisecond, func(time.Time) tea.Msg {
		return aiThinkingTickMsg{}
	})
}

func (m *appModel) openModelPicker() bool {
	options := availableAIModels()
	if len(options) == 0 {
		m.status = "Set GEMINI_API_KEY or OPENAI_API_KEY to enable AI models."
		return false
	}
	if len(options) == 1 || onlyOneAIProvider(options) {
		provider, model := options[0].provider, options[0].model
		m.aiClient.SetModel(provider, model)
		m.selectedAIModel = m.aiClient.Model()
		m.status = fmt.Sprintf("AI model set to %s", m.selectedAIModel)
		return true
	}

	m.modelPicker = newModelPickerModel(options, m.selectedAIModel)
	m.modelPicker.SetSize(m.width, m.height)
	m.pushScreen(m.screen)
	m.screen = screenModelPicker
	return true
}

func (m *appModel) startAIErrorExplanation(index int) tea.Cmd {
	if index < 0 || index >= len(m.tabs) || m.aiClient == nil || m.aiClient.Disabled() {
		if index >= 0 && index < len(m.tabs) {
			m.tabs[index].status = "AI decoder requires GEMINI_API_KEY or OPENAI_API_KEY."
		}
		return nil
	}

	traceback := strings.TrimSpace(m.tabs[index].output.StderrText())
	if traceback == "" {
		m.tabs[index].status = "No error output to analyze."
		return nil
	}
	if !m.claimAICooldown(index) {
		return nil
	}

	m.aiLoading = true
	m.aiThinkingFrame = 0
	m.aiThinkingTab = index
	m.tabs[index].status = "Analyzing error..."
	code := m.tabs[index].editor.Content()
	return tea.Batch(scheduleAIThinkingTick(), func() tea.Msg {
		content, err := m.aiClient.ExplainError(context.Background(), code, traceback)
		return aiResponseMsg{
			tabIndex:    index,
			text:        content,
			err:         err,
			header:      "AI Explanation",
			headerColor: "14",
		}
	})
}

func (m *appModel) startAIHint(index int) tea.Cmd {
	if index < 0 || index >= len(m.tabs) {
		return nil
	}
	if m.aiClient == nil || m.aiClient.Disabled() {
		m.tabs[index].status = "AI hints require GEMINI_API_KEY or OPENAI_API_KEY."
		return nil
	}

	tab := &m.tabs[index]
	source := tab.editor.Content()
	if tab.lastHintSource == source && tab.hintLevel > 0 {
		tab.hintLevel = min(tab.hintLevel+1, 3)
	} else {
		tab.hintLevel = 1
	}
	tab.lastHintSource = source
	if !m.claimAICooldown(index) {
		return nil
	}
	m.aiLoading = true
	m.aiThinkingFrame = 0
	m.aiThinkingTab = index
	tab.status = fmt.Sprintf("Generating hint (level %d)...", tab.hintLevel)

	currentError := tab.output.StderrText()
	currentLine := tab.editor.ExecutionLine()
	hintLevel := tab.hintLevel
	return tea.Batch(scheduleAIThinkingTick(), func() tea.Msg {
		content, err := m.aiClient.GetHint(context.Background(), source, currentError, currentLine, hintLevel)
		return aiResponseMsg{
			tabIndex:    index,
			text:        content,
			err:         err,
			header:      fmt.Sprintf("AI Hint (Level %d)", hintLevel),
			headerColor: "11",
		}
	})
}

func (m *appModel) claimAICooldown(index int) bool {
	if index < 0 || index >= len(m.tabs) {
		return false
	}
	now := time.Now()
	if now.Before(m.tabs[index].aiCooldownUntil) {
		return false
	}
	m.tabs[index].aiCooldownUntil = now.Add(10 * time.Second)
	return true
}

func formatAIError(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "AI request timed out. Check your connection."
	case errors.Is(err, ai.ErrParseResponse):
		return "AI response could not be parsed."
	default:
		var statusErr ai.ErrHTTPStatus
		if errors.As(err, &statusErr) {
			switch {
			case statusErr.StatusCode == 429 && statusErr.Provider == "gemini":
				return "Gemini rate limit reached. Wait a moment and try again."
			case statusErr.StatusCode == 429 && statusErr.Provider == "openai":
				return "OpenAI rate limit reached. Wait a moment and try again."
			case statusErr.StatusCode >= 400 && statusErr.StatusCode < 500:
				return fmt.Sprintf("AI service returned an error (HTTP %d). Check your API key.", statusErr.StatusCode)
			case statusErr.StatusCode >= 500:
				return "AI service is temporarily unavailable. Try again later."
			}
		}
		return "AI service returned an error. Try again later."
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

func (m *appModel) defaultCreateDir() string {
	if m.hasActiveTab() {
		return filepath.Dir(m.tabs[m.activeTab].path)
	}
	return pickerStartPath("")
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func availableAIModels() []aiModelOption {
	var items []aiModelOption
	if strings.TrimSpace(os.Getenv("GEMINI_API_KEY")) != "" {
		items = append(items,
			aiModelOption{provider: "gemini", model: "gemini-2.5-flash", detail: "(recommended, fast)"},
			aiModelOption{provider: "gemini", model: "gemini-2.0-flash", detail: "(stable alternative)"},
			aiModelOption{provider: "gemini", model: "gemini-1.5-flash", detail: "(alternative)"},
			aiModelOption{provider: "gemini", model: "gemini-1.5-pro", detail: "(more capable, lower rate limit)"},
		)
	}
	if strings.TrimSpace(os.Getenv("OPENAI_API_KEY")) != "" {
		items = append(items,
			aiModelOption{provider: "openai", model: "gpt-4o-mini", detail: "(affordable)"},
			aiModelOption{provider: "openai", model: "gpt-4o", detail: "(most capable)"},
		)
	}
	return items
}

func onlyOneAIProvider(items []aiModelOption) bool {
	if len(items) == 0 {
		return false
	}
	provider := items[0].provider
	for _, item := range items[1:] {
		if item.provider != provider {
			return false
		}
	}
	return true
}

func renderFooter(width int, left, model, thinking string) string {
	leftRendered := mutedStyle.Render(left)
	rightText := model
	if thinking != "" {
		if rightText != "" {
			rightText = thinking + "  " + rightText
		} else {
			rightText = thinking
		}
	}
	if rightText == "" {
		return lipgloss.NewStyle().Width(width).Render(leftRendered)
	}

	rightRendered := mutedStyle.Render(rightText)
	leftWidth := lipgloss.Width(leftRendered)
	rightWidth := lipgloss.Width(rightRendered)
	if leftWidth+rightWidth+1 > width {
		return lipgloss.NewStyle().Width(width).Render(leftRendered)
	}

	return lipgloss.NewStyle().Width(width).Render(leftRendered + strings.Repeat(" ", width-leftWidth-rightWidth) + rightRendered)
}

var aiThinkingFrames = []string{
	"[Thinking   ]",
	"[Thinking.  ]",
	"[Thinking.. ]",
	"[Thinking...]",
}

func (m *appModel) aiStatusText() string {
	if !m.aiLoading {
		return ""
	}
	if m.aiThinkingFrame < 0 || m.aiThinkingFrame >= len(aiThinkingFrames) {
		return aiThinkingFrames[0]
	}
	return aiThinkingFrames[m.aiThinkingFrame]
}
