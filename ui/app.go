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
	screenThemePicker
	screenCommandPalette
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

type aiStreamEvent struct {
	requestID   int
	tabIndex    int
	chunk       string
	done        bool
	err         error
	header      string
	headerColor string
}

type aiStreamMsg struct {
	event aiStreamEvent
}

type promptMode int

const (
	promptNone promptMode = iota
	promptFind
	promptGoto
)

type promptField struct {
	label string
	value []rune
}

type inlinePrompt struct {
	mode        promptMode
	title       string
	hint        string
	fields      []promptField
	activeField int
}

type editorTab struct {
	id                int
	path              string
	editor            editor.Model
	output            outputModel
	status            string
	debounceToken     int
	activeRunID       int
	pythonInterpreter string
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
	tabBarWidth  int
	editorBodyY  int
	contentX     int
	sidebarX     int
	sidebarY     int
	sidebarWidth int
	editorX      int
	editorWidth  int
}

const editorChromeRows = 4

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
	themePicker        themePickerModel
	commandPalette     commandPaletteModel
	runner             *runner.Manager
	aiClient           *ai.Client
	selectedAIModel    string
	selectedTheme      string
	config             appConfig
	customThemeLoaded  bool
	aiLoading          bool
	aiThinkingFrame    int
	aiThinkingTab      int
	aiRequestSeq       int
	activeAIRequestID  int
	aiEvents           chan aiStreamEvent
	status             string
	focus              string
	workspaceRoot      string
	sidebarEntries     []sidebarEntry
	sidebarScroll      int
	gitStatus          map[string]string
	tabScroll          int
	tabs               []editorTab
	layout             editorLayout
	pythonInterpreters []runner.PythonInterpreter
	prompt             inlinePrompt
}

func NewApp() tea.Model {
	interpreters, _ := runner.DiscoverPythonInterpreters()
	cfg := loadAppConfig()
	aiClient := ai.NewClient(cfg.OpenAIKey, cfg.GeminiKey, cfg.AnthropicKey, cfg.AIPreferredModel)
	registerBuiltInThemeAliases()
	customThemeLoaded := loadCustomTheme()
	editor.SetTheme("monokai")
	return &appModel{
		screen:             screenWelcome,
		prevScreen:         screenWelcome,
		activeTab:          -1,
		welcome:            newWelcomeModel(),
		picker:             filepicker.New("", filepicker.PickFile),
		newFile:            newNewFileModel(pickerStartPath("")),
		interpreterPicker:  newInterpreterPickerModel(nil, ""),
		modelPicker:        newModelPickerModel(nil, ""),
		themePicker:        newThemePickerModel(nil, ""),
		commandPalette:     newCommandPaletteModel(),
		runner:             runner.New(),
		aiClient:           aiClient,
		aiEvents:           make(chan aiStreamEvent, 256),
		selectedAIModel:    aiClient.Model(),
		selectedTheme:      "monokai",
		config:             cfg,
		customThemeLoaded:  customThemeLoaded,
		aiThinkingTab:      -1,
		status:             "Choose a file to begin.",
		focus:              "editor",
		gitStatus:          map[string]string{},
		pythonInterpreters: interpreters,
	}
}

func (m *appModel) Init() tea.Cmd {
	return tea.Batch(waitForRunnerEvent(m.runner), m.welcome.Init())
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
			m.tabs[idx].output.SetStatus("Running...")
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
			case msg.TimedOut:
				m.tabs[idx].editor.ClearExecution()
				m.tabs[idx].output.SetStatus("Run timed out")
				if msg.Err != nil {
					m.tabs[idx].output.Append(msg.Err.Error(), true)
				}
				m.tabs[idx].status = "Run timed out"
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

	case aiStreamMsg:
		event := msg.event
		if event.requestID != m.activeAIRequestID {
			return m, nil
		}
		if event.tabIndex >= 0 && event.tabIndex < len(m.tabs) {
			tab := &m.tabs[event.tabIndex]
			if event.chunk != "" {
				tab.output.AppendAIChunk(event.chunk)
				tab.status = "Streaming AI response..."
			}
			if event.done {
				tab.output.FinishAIBlock()
				m.aiLoading = false
				m.aiThinkingFrame = 0
				m.aiThinkingTab = -1
				m.activeAIRequestID = 0
				if event.err != nil {
					tab.output.AppendAIChunk(formatAIError(event.err))
					tab.status = "AI request failed"
				} else {
					tab.status = "AI response ready"
				}
				return m, nil
			}
		}
		return m, waitForAIStream(m.aiEvents)

	case aiThinkingTickMsg:
		if !m.aiLoading {
			return m, nil
		}
		m.aiThinkingFrame = (m.aiThinkingFrame + 1) % len(aiThinkingFrames)
		return m, scheduleAIThinkingTick()

	case CommandPaletteActionMsg:
		return m, m.executeCommandAction(msg.Action)

	case filepicker.SelectedMsg:
		if msg.Mode == filepicker.PickDirectory {
			m.workspaceRoot = msg.Path
			m.picker = filepicker.NewRooted(msg.Path, filepicker.PickFile)
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
		switch m.screen {
		case screenWelcome:
			var cmd tea.Cmd
			m.welcome, cmd = m.welcome.Update(msg)
			return m, cmd
		case screenPicker:
			var cmd tea.Cmd
			m.picker, cmd = m.picker.Update(msg)
			return m, cmd
		case screenNewFile:
			var cmd tea.Cmd
			m.newFile, cmd = m.newFile.Update(msg)
			return m, cmd
		case screenInterpreterPicker:
			var cmd tea.Cmd
			m.interpreterPicker, cmd = m.interpreterPicker.Update(msg)
			return m, cmd
		case screenModelPicker:
			var cmd tea.Cmd
			m.modelPicker, cmd = m.modelPicker.Update(msg)
			return m, cmd
		case screenThemePicker:
			var cmd tea.Cmd
			m.themePicker, cmd = m.themePicker.Update(msg)
			return m, cmd
		case screenCommandPalette:
			return m, nil
		case screenEditor:
			if m.hasActiveTab() {
				var cmd tea.Cmd
				m.tabs[m.activeTab].editor, cmd = m.tabs[m.activeTab].editor.Update(msg)
				return m, cmd
			}
		}
		return m, nil
	}

	if m.screen == screenCommandPalette {
		var close bool
		var cmd tea.Cmd
		m.commandPalette, close, cmd = m.commandPalette.Update(key)
		if close {
			m.screen = m.prevScreen
		}
		return m, cmd
	}

	if m.screen == screenEditor && m.hasActiveTab() && m.prompt.mode != promptNone {
		if key.Paste {
			m.appendToPromptField(string(key.Runes))
			return m, nil
		}
		if handled, cmd := m.handlePromptKey(key); handled {
			return m, cmd
		}
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
	case "ctrl+p", "?":
		if m.screen == screenEditor || m.screen == screenWelcome {
			m.commandPalette.SetSize(m.width, m.height)
			m.commandPalette.Open(key.String() == "?")
			m.prevScreen = m.screen
			m.screen = screenCommandPalette
			return m, nil
		}
	case "ctrl+e":
		if m.screen == screenEditor && m.hasActiveTab() && !m.tabs[m.activeTab].output.InputFocused() {
			cmd := m.startAIErrorExplanation(m.activeTab)
			return m, cmd
		}
	case "ctrl+f":
		if m.screen == screenEditor && m.hasActiveTab() && !m.tabs[m.activeTab].output.InputFocused() {
			m.openFindPrompt()
			return m, nil
		}
	case "ctrl+g":
		if m.screen == screenEditor && m.hasActiveTab() && !m.tabs[m.activeTab].output.InputFocused() {
			m.openGotoPrompt()
			return m, nil
		}
	case "ctrl+h":
		if m.screen == screenEditor && m.hasActiveTab() && !m.tabs[m.activeTab].output.InputFocused() {
			cmd := m.startAIHint(m.activeTab)
			return m, cmd
		}
	case "ctrl+z":
		if m.screen == screenEditor && m.hasActiveTab() && !m.tabs[m.activeTab].output.InputFocused() {
			if m.tabs[m.activeTab].editor.Undo() {
				m.tabs[m.activeTab].status = "Undo"
				return m, scheduleRun(m.bumpDebounce(m.activeTab), m.tabs[m.activeTab].id)
			}
			m.tabs[m.activeTab].status = "Nothing to undo"
			return m, nil
		}
	case "ctrl+y":
		if m.screen == screenEditor && m.hasActiveTab() && !m.tabs[m.activeTab].output.InputFocused() {
			if m.tabs[m.activeTab].editor.Redo() {
				m.tabs[m.activeTab].status = "Redo"
				return m, scheduleRun(m.bumpDebounce(m.activeTab), m.tabs[m.activeTab].id)
			}
			m.tabs[m.activeTab].status = "Nothing to redo"
			return m, nil
		}
	case "ctrl+m", "alt+m":
		if m.screen == screenEditor {
			if handled := m.openModelPicker(); handled {
				return m, nil
			}
		}
	case "alt+t":
		if m.screen == screenEditor {
			m.openThemePicker()
			return m, nil
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
		case screenPicker, screenNewFile, screenInterpreterPicker, screenModelPicker, screenThemePicker:
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
			if m.prompt.mode != promptNone {
				m.prompt = inlinePrompt{}
				m.tabs[m.activeTab].status = "Prompt closed"
				return m, nil
			}
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
				m.refreshWorkspaceView()
				tab.status = fmt.Sprintf("Saved %s", filepath.Base(tab.path))
				m.startRunForTab(m.activeTab, "Saved and running...")
			}
		}
		return m, nil
	case "alt+b":
		if m.screen == screenEditor && m.hasActiveTab() && !m.tabs[m.activeTab].output.InputFocused() {
			if m.tabs[m.activeTab].editor.ToggleBlockSelection() {
				m.tabs[m.activeTab].status = "Block selection enabled"
			} else {
				m.tabs[m.activeTab].status = "Block selection disabled"
			}
			return m, nil
		}
	}

	switch m.screen {
	case screenWelcome:
		var cmd tea.Cmd
		m.welcome, cmd = m.welcome.Update(msg)
		if key.String() == "enter" || key.String() == "n" {
			switch m.welcome.Selected() {
			case "Open File":
				if key.String() == "n" {
					break
				}
				m.welcome.SetMessage("")
				m.picker = filepicker.New(pickerStartPath(""), filepicker.PickFile)
				m.picker.SetSize(m.width-6, m.height-6)
				m.pushScreen(screenWelcome)
				m.screen = screenPicker
			case "Open Folder":
				if key.String() == "n" {
					break
				}
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
				if key.String() == "n" {
					m.welcome.SetMessage("")
					m.newFile = newNewFileModel(m.defaultCreateDir())
					m.newFile.SetSize(m.width, m.height)
					m.pushScreen(screenWelcome)
					m.screen = screenNewFile
					break
				}
				m.welcome.SetMessage("Recent Files is not implemented yet.")
			}
			if key.String() == "n" && m.screen == screenWelcome {
				m.welcome.SetMessage("")
				m.newFile = newNewFileModel(m.defaultCreateDir())
				m.newFile.SetSize(m.width, m.height)
				m.pushScreen(screenWelcome)
				m.screen = screenNewFile
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

	case screenThemePicker:
		var cmd tea.Cmd
		m.themePicker, cmd = m.themePicker.Update(msg)
		if key.String() == "enter" {
			selected := m.themePicker.Selected()
			if selected != "" {
				m.selectedTheme = selected
				editor.SetTheme(selected)
				m.status = fmt.Sprintf("Theme set to %s", selected)
			}
			m.screen = m.popScreen()
			return m, nil
		}
		return m, cmd

	case screenCommandPalette:
		return m, nil

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
	if m.screen == screenCommandPalette {
		return lipgloss.Place(
			max(1, m.width),
			max(1, m.height),
			lipgloss.Center,
			lipgloss.Center,
			m.commandPalette.View(),
		)
	}

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
	case screenThemePicker:
		return m.themePicker.View()
	case screenEditor:
		if !m.hasActiveTab() {
			return m.welcome.View()
		}

		tab := m.tabs[m.activeTab]
		panelWidth := max(20, m.width-4)
		editorBodyHeight := max(3, m.editorHeight-editorChromeRows)
		sidebarOuterWidth := min(32, max(20, panelWidth/4))
		editorOuterWidth := max(20, panelWidth-sidebarOuterWidth-1)
		sidebarContentWidth := max(10, sidebarOuterWidth-panelStyle.GetHorizontalFrameSize())
		editorContentWidth := max(10, editorOuterWidth-activePanelStyle.GetHorizontalFrameSize())
		topBodyHeight := lipgloss.Height("x") + lipgloss.Height("x") + editorBodyHeight
		tab.editor.SetSize(editorContentWidth, editorBodyHeight)
		tab.output.SetSize(max(10, panelWidth-4), max(2, m.outputHeight-2))
		m.refreshWorkspaceView()
		m.ensureSidebarEntryVisible(tab.path, editorBodyHeight)
		tabBar := lipgloss.NewStyle().
			Width(editorContentWidth).
			BorderBottom(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("8")).
			Render(m.renderTabBar(editorContentWidth))
		header := lipgloss.NewStyle().
			Width(editorContentWidth).
			BorderBottom(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("8")).
			Render(titleStyle.Render(filepath.Base(tab.path)) + m.renderGitBadge(tab.path) + "  " + mutedStyle.Render(tab.status) + m.renderInterpreterBadge(tab))
		topBodyHeight = lipgloss.Height(tabBar) + lipgloss.Height(header) + editorBodyHeight
		sidebar := panelStyle.Copy().Width(sidebarOuterWidth).Height(topBodyHeight).Render(
			m.renderSidebar(sidebarContentWidth, topBodyHeight),
		)
		editorPane := activePanelStyle.Width(editorOuterWidth).Height(topBodyHeight).Render(
			lipgloss.JoinVertical(lipgloss.Left, tabBar, header, tab.editor.View()),
		)
		top := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, " ", editorPane)

		m.layout = editorLayout{
			panelX:       appPaddingStyle.GetHorizontalFrameSize() / 2,
			panelY:       appPaddingStyle.GetVerticalFrameSize() / 2,
			panelWidth:   panelWidth,
			topHeight:    lipgloss.Height(top),
			bottomHeight: max(3, m.outputHeight) + panelStyle.GetVerticalFrameSize(),
			tabBarY:      appPaddingStyle.GetVerticalFrameSize() / 2,
			tabBarX:      appPaddingStyle.GetHorizontalFrameSize()/2 + sidebarOuterWidth + 1 + activePanelStyle.GetHorizontalFrameSize()/2,
			tabBarWidth:  editorContentWidth,
			sidebarX:     appPaddingStyle.GetHorizontalFrameSize() / 2,
			sidebarY:     appPaddingStyle.GetVerticalFrameSize() / 2,
			sidebarWidth: sidebarOuterWidth,
		}
		m.layout.bottomY = m.layout.panelY + m.layout.topHeight
		m.layout.editorBodyY = m.layout.panelY + lipgloss.Height(tabBar) + lipgloss.Height(header) + activePanelStyle.GetVerticalFrameSize()/2 - 1
		m.layout.editorX = m.layout.tabBarX
		m.layout.editorWidth = editorContentWidth
		m.layout.contentX = m.layout.panelX + panelStyle.GetHorizontalFrameSize()/2

		bottomStyle := panelStyle.Copy().Width(panelWidth).Height(max(3, m.outputHeight))
		bottom := bottomStyle.Render(tab.output.View())
		footer := renderFooter(panelWidth, m.contextualFooter(), m.selectedAIModel, m.aiStatusText())
		parts := []string{top, bottom}
		if prompt := m.renderPrompt(panelWidth); prompt != "" {
			parts = append(parts, prompt)
		}
		parts = append(parts, footer)
		return appPaddingStyle.Render(lipgloss.JoinVertical(lipgloss.Left, parts...))
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
	m.themePicker.SetSize(m.width, m.height)
	m.commandPalette.SetSize(m.width, m.height)
	for i := range m.tabs {
		m.tabs[i].editor.SetSize(max(10, m.width-8), max(3, m.editorHeight-editorChromeRows))
		m.tabs[i].output.SetSize(max(10, m.width-8), max(2, m.outputHeight-2))
	}
}

func (m *appModel) openFile(path string) (int, error) {
	path = pickerStartPath(path)
	if m.workspaceRoot == "" {
		m.workspaceRoot = filepath.Dir(path)
	}
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
	tab.editor.SetSize(max(10, m.width-8), max(3, m.editorHeight-editorChromeRows))
	tab.output.SetSize(max(10, m.width-8), max(2, m.outputHeight-2))

	m.tabs = append(m.tabs, tab)
	m.refreshWorkspaceView()
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
	m.prompt = inlinePrompt{}
	m.screen = screenEditor
	m.focus = "editor"
	if m.workspaceRoot == "" {
		m.workspaceRoot = filepath.Dir(m.tabs[index].path)
	}
	m.refreshWorkspaceView()
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
	m.refreshWorkspaceView()
	m.activateTab(m.activeTab)
}

func (m *appModel) startRunForTab(index int, status string) {
	if index < 0 || index >= len(m.tabs) {
		return
	}
	tab := &m.tabs[index]
	opts := runner.RunOptions{
		MaxRuntime: runner.DefaultMaxRuntime,
	}
	if strings.EqualFold(filepath.Ext(tab.path), ".py") {
		opts.PythonInterpreter = tab.pythonInterpreter
	}
	tab.activeRunID = m.runner.Start(tab.path, tab.editor.Content(), opts)
	tab.editor.ClearExecution()
	tab.output.StartSession(status, m.runCommandLabel(*tab))
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

	items := m.tabItems()
	m.adjustTabScroll(width)
	start, end := m.visibleTabRange(width)
	rendered := make([]string, 0, end-start+3)
	rendered = append(rendered, accentStyle.Render(fmt.Sprintf("Tabs %d:", len(m.tabs))))
	if start > 0 {
		rendered = append(rendered, mutedStyle.Render("‹"))
	}
	for i := start; i < end; i++ {
		item := items[i]
		label := item.label
		if i == m.activeTab {
			label = accentStyle.Render("[" + label + "]")
		} else {
			label = mutedStyle.Render(label)
		}
		rendered = append(rendered, label)
	}
	if end < len(items) {
		rendered = append(rendered, mutedStyle.Render("›"))
	}
	bar := strings.Join(rendered, "  ")
	if lipgloss.Width(bar) > width {
		bar = truncateLabel(bar, max(1, width))
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
		if status := m.gitStatus[tab.path]; status != "" {
			label = status + " " + label
		}
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

func (m *appModel) adjustTabScroll(width int) {
	if m.activeTab < 0 || m.activeTab >= len(m.tabs) {
		m.tabScroll = 0
		return
	}
	start, end := m.visibleTabRange(width)
	if m.activeTab < start {
		m.tabScroll = m.activeTab
		return
	}
	if m.activeTab >= end {
		m.tabScroll = m.activeTab
	}
}

func (m *appModel) visibleTabRange(width int) (int, int) {
	items := m.tabItems()
	if len(items) == 0 {
		return 0, 0
	}
	if m.tabScroll < 0 {
		m.tabScroll = 0
	}
	if m.tabScroll >= len(items) {
		m.tabScroll = len(items) - 1
	}
	start := m.tabScroll
	used := lipgloss.Width(fmt.Sprintf("Tabs %d:", len(items)))
	if start > 0 {
		used += lipgloss.Width("  ‹")
	}
	end := start
	for end < len(items) {
		itemWidth := lipgloss.Width(items[end].label) + 4
		extra := itemWidth
		if end < len(items)-1 {
			extra += 2
		}
		if used+extra > width && end > start {
			break
		}
		used += extra
		end++
		if used >= width {
			break
		}
	}
	if end < len(items) {
		for end > start && used+lipgloss.Width("  ›") > width {
			end--
			used -= lipgloss.Width(items[end].label) + 6
		}
	}
	if end <= start {
		end = min(len(items), start+1)
	}
	return start, end
}

func (m *appModel) renderGitBadge(path string) string {
	status := m.gitStatus[path]
	if status == "" {
		return ""
	}
	return "  " + mutedStyle.Render("["+status+"]")
}

func (m *appModel) tabHitboxes() []tabHitbox {
	items := m.tabItems()
	hitboxes := make([]tabHitbox, 0, len(items))
	start, end := m.visibleTabRange(m.layout.tabBarWidth)
	offset := lipgloss.Width(fmt.Sprintf("Tabs %d:", len(m.tabs))) + 2
	if start > 0 {
		offset += lipgloss.Width("‹") + 2
	}
	for _, item := range items[start:end] {
		// Padding(0,1) + MarginRight(1)
		width := lipgloss.Width(item.label) + 3
		hitboxes = append(hitboxes, tabHitbox{
			index: item.index,
			start: offset,
			end:   offset + width,
		})
		offset += width + 2
	}
	return hitboxes
}

func (m *appModel) handleMouseClick(x, y int) tea.Cmd {
	if !m.hasActiveTab() {
		return nil
	}

	layout := m.currentEditorLayout()

	if x >= layout.sidebarX && x < layout.sidebarX+layout.sidebarWidth && y >= layout.sidebarY && y < layout.sidebarY+layout.topHeight {
		row := y - layout.sidebarY - 2
		visible := m.visibleSidebarEntries(max(1, layout.topHeight-4))
		if row >= 0 && row < len(visible) {
			entry := visible[row]
			if !entry.isDir {
				if _, err := m.openFile(entry.path); err == nil {
					return nil
				}
			}
		}
		return nil
	}

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

		if y >= layout.editorBodyY && x >= layout.editorX && x < layout.editorX+layout.editorWidth {
			m.tabs[m.activeTab].editor.SetCursorFromView(y-layout.editorBodyY, x-layout.editorX)
			m.tabs[m.activeTab].editor.BeginSelectionFromView(y-layout.editorBodyY, x-layout.editorX)
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
		if y >= layout.editorBodyY && x >= layout.editorX && x < layout.editorX+layout.editorWidth {
			m.tabs[m.activeTab].editor.UpdateSelectionFromView(y-layout.editorBodyY, x-layout.editorX)
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

func (m *appModel) runCommandLabel(tab editorTab) string {
	base := filepath.Base(tab.path)
	switch strings.ToLower(filepath.Ext(tab.path)) {
	case ".py":
		interpreter := "python"
		if tab.pythonInterpreter != "" {
			interpreter = filepath.Base(tab.pythonInterpreter)
		}
		return "➜ " + interpreter + " " + base
	case ".go":
		return "➜ go run " + base
	default:
		return "➜ " + base
	}
}

func (m *appModel) handleMouseScroll(x, y, delta int) {
	if !m.hasActiveTab() {
		return
	}

	layout := m.currentEditorLayout()
	if x >= layout.sidebarX && x < layout.sidebarX+layout.sidebarWidth && y >= layout.sidebarY && y < layout.sidebarY+layout.topHeight {
		m.sidebarScroll += delta
		if m.sidebarScroll < 0 {
			m.sidebarScroll = 0
		}
		if len(m.sidebarEntries) > 0 {
			m.sidebarScroll = min(m.sidebarScroll, max(0, len(m.sidebarEntries)-1))
		}
		return
	}
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
	panelWidth := max(20, m.width-4)
	sidebarOuterWidth := min(32, max(20, panelWidth/4))
	editorOuterWidth := max(20, panelWidth-sidebarOuterWidth-1)
	editorContentWidth := max(10, editorOuterWidth-activePanelStyle.GetHorizontalFrameSize())
	return editorLayout{
		panelX:       appPaddingStyle.GetHorizontalFrameSize() / 2,
		panelY:       appPaddingStyle.GetVerticalFrameSize() / 2,
		panelWidth:   panelWidth,
		topHeight:    max(3, m.editorHeight-editorChromeRows) + editorChromeRows + activePanelStyle.GetVerticalFrameSize(),
		bottomY:      appPaddingStyle.GetVerticalFrameSize()/2 + max(3, m.editorHeight-editorChromeRows) + editorChromeRows + activePanelStyle.GetVerticalFrameSize(),
		bottomHeight: max(3, m.outputHeight) + panelStyle.GetVerticalFrameSize(),
		tabBarY:      appPaddingStyle.GetVerticalFrameSize() / 2,
		tabBarX:      appPaddingStyle.GetHorizontalFrameSize()/2 + sidebarOuterWidth + 1 + activePanelStyle.GetHorizontalFrameSize()/2,
		tabBarWidth:  editorContentWidth,
		editorBodyY:  appPaddingStyle.GetVerticalFrameSize()/2 + editorChromeRows - 1,
		contentX:     appPaddingStyle.GetHorizontalFrameSize()/2 + panelStyle.GetHorizontalFrameSize()/2,
		sidebarX:     appPaddingStyle.GetHorizontalFrameSize() / 2,
		sidebarY:     appPaddingStyle.GetVerticalFrameSize() / 2,
		sidebarWidth: sidebarOuterWidth,
		editorX:      appPaddingStyle.GetHorizontalFrameSize()/2 + sidebarOuterWidth + 1 + activePanelStyle.GetHorizontalFrameSize()/2,
		editorWidth:  editorContentWidth,
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

func waitForAIStream(events <-chan aiStreamEvent) tea.Cmd {
	return func() tea.Msg {
		return aiStreamMsg{event: <-events}
	}
}

func (m *appModel) openFindPrompt() {
	findValue := ""
	if m.tabs[m.activeTab].editor.HasSelection() {
		findValue = m.tabs[m.activeTab].editor.SelectedText()
	}
	m.prompt = inlinePrompt{
		mode:  promptFind,
		title: "Find / Replace",
		hint:  "enter next • shift+enter previous • alt+enter replace all • tab switch field • esc close",
		fields: []promptField{
			{label: "Find", value: []rune(findValue)},
			{label: "Replace"},
		},
	}
}

func (m *appModel) openGotoPrompt() {
	m.prompt = inlinePrompt{
		mode:  promptGoto,
		title: "Jump To Line",
		hint:  "type a 1-based line number and press enter",
		fields: []promptField{
			{label: "Line", value: []rune(fmt.Sprintf("%d", m.tabs[m.activeTab].editor.CurrentCursorLine()))},
		},
	}
}

func (m *appModel) handlePromptKey(key tea.KeyMsg) (bool, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.prompt = inlinePrompt{}
		m.tabs[m.activeTab].status = "Prompt closed"
		return true, nil
	case "tab":
		if len(m.prompt.fields) > 0 {
			m.prompt.activeField = (m.prompt.activeField + 1) % len(m.prompt.fields)
		}
		return true, nil
	case "shift+tab":
		if len(m.prompt.fields) > 0 {
			m.prompt.activeField = (m.prompt.activeField - 1 + len(m.prompt.fields)) % len(m.prompt.fields)
		}
		return true, nil
	case "backspace":
		field := &m.prompt.fields[m.prompt.activeField]
		if len(field.value) > 0 {
			field.value = field.value[:len(field.value)-1]
		}
		return true, nil
	case "enter":
		return true, m.submitPrompt(true, false)
	case "shift+enter":
		return true, m.submitPrompt(false, false)
	case "alt+enter":
		return true, m.submitPrompt(true, true)
	}
	if key.Type == tea.KeyRunes || key.Type == tea.KeySpace {
		m.appendToPromptField(key.String())
		return true, nil
	}
	return false, nil
}

func (m *appModel) submitPrompt(forward, replaceAll bool) tea.Cmd {
	switch m.prompt.mode {
	case promptFind:
		find := strings.TrimSpace(string(m.prompt.fields[0].value))
		replace := string(m.prompt.fields[1].value)
		if find == "" {
			m.tabs[m.activeTab].status = "Enter text to find"
			return nil
		}
		if replaceAll && replace != "" {
			count := m.tabs[m.activeTab].editor.ReplaceAll(find, replace)
			if count == 0 {
				m.tabs[m.activeTab].status = fmt.Sprintf("No matches for %q", find)
				return nil
			}
			m.tabs[m.activeTab].status = fmt.Sprintf("Replaced %d match(es)", count)
			return scheduleRun(m.bumpDebounce(m.activeTab), m.tabs[m.activeTab].id)
		}
		if replace != "" && m.tabs[m.activeTab].editor.ReplaceSelection(find, replace) {
			cmd := scheduleRun(m.bumpDebounce(m.activeTab), m.tabs[m.activeTab].id)
			if m.tabs[m.activeTab].editor.FindNext(find, true) {
				m.tabs[m.activeTab].status = fmt.Sprintf("Replaced %q and moved to next match", find)
			} else {
				m.tabs[m.activeTab].status = fmt.Sprintf("Replaced final %q", find)
			}
			return cmd
		}
		if m.tabs[m.activeTab].editor.FindNext(find, forward) {
			if forward {
				m.tabs[m.activeTab].status = fmt.Sprintf("Found next %q", find)
			} else {
				m.tabs[m.activeTab].status = fmt.Sprintf("Found previous %q", find)
			}
			return nil
		}
		m.tabs[m.activeTab].status = fmt.Sprintf("No matches for %q", find)
		return nil
	case promptGoto:
		lineText := strings.TrimSpace(string(m.prompt.fields[0].value))
		line := 0
		_, err := fmt.Sscanf(lineText, "%d", &line)
		if err != nil || line <= 0 {
			m.tabs[m.activeTab].status = "Enter a valid line number"
			return nil
		}
		if !m.tabs[m.activeTab].editor.JumpToLine(line) {
			m.tabs[m.activeTab].status = fmt.Sprintf("Line %d is out of range", line)
			return nil
		}
		m.tabs[m.activeTab].status = fmt.Sprintf("Jumped to line %d", line)
		m.prompt = inlinePrompt{}
		return nil
	default:
		return nil
	}
}

func (m *appModel) appendToPromptField(text string) {
	if m.prompt.mode == promptNone || len(m.prompt.fields) == 0 || text == "" {
		return
	}
	m.prompt.fields[m.prompt.activeField].value = append(m.prompt.fields[m.prompt.activeField].value, []rune(text)...)
}

func (m *appModel) renderPrompt(width int) string {
	if m.prompt.mode == promptNone {
		return ""
	}
	parts := []string{accentStyle.Render(m.prompt.title)}
	for i, field := range m.prompt.fields {
		value := string(field.value)
		label := field.label + ": "
		style := mutedStyle
		if i == m.prompt.activeField {
			style = accentStyle
			value += "_"
		}
		parts = append(parts, style.Render(label+value))
	}
	if m.prompt.hint != "" {
		parts = append(parts, mutedStyle.Render(m.prompt.hint))
	}
	return panelStyle.Copy().Width(width).Render(strings.Join(parts, "  "))
}

func (m *appModel) openModelPicker() bool {
	options := availableAIModels(m.config)
	if len(options) == 0 {
		m.status = "Set Gemini, OpenAI, or Anthropic keys in env vars or config.toml to enable AI models."
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

func (m *appModel) openThemePicker() {
	options := availableEditorThemes(m.customThemeLoaded)
	m.themePicker = newThemePickerModel(options, m.selectedTheme)
	m.themePicker.SetSize(m.width, m.height)
	m.pushScreen(m.screen)
	m.screen = screenThemePicker
}

func (m *appModel) executeCommandAction(action string) tea.Cmd {
	switch action {
	case "save":
		if m.screen == screenEditor && m.hasActiveTab() {
			tab := &m.tabs[m.activeTab]
			if err := os.WriteFile(tab.path, []byte(tab.editor.Content()), 0o644); err != nil {
				tab.status = err.Error()
				return nil
			}
			tab.editor.MarkSaved()
			m.refreshWorkspaceView()
			tab.status = fmt.Sprintf("Saved %s", filepath.Base(tab.path))
			m.startRunForTab(m.activeTab, "Saved and running...")
		}
	case "undo":
		if m.screen == screenEditor && m.hasActiveTab() && m.tabs[m.activeTab].editor.Undo() {
			m.tabs[m.activeTab].status = "Undo"
			return scheduleRun(m.bumpDebounce(m.activeTab), m.tabs[m.activeTab].id)
		}
	case "redo":
		if m.screen == screenEditor && m.hasActiveTab() && m.tabs[m.activeTab].editor.Redo() {
			m.tabs[m.activeTab].status = "Redo"
			return scheduleRun(m.bumpDebounce(m.activeTab), m.tabs[m.activeTab].id)
		}
	case "find":
		if m.screen == screenEditor && m.hasActiveTab() {
			m.openFindPrompt()
		}
	case "goto":
		if m.screen == screenEditor && m.hasActiveTab() {
			m.openGotoPrompt()
		}
	case "open_file":
		root := pickerStartPath("")
		if m.hasActiveTab() {
			root = pickerStartPath(filepath.Dir(m.tabs[m.activeTab].path))
		}
		m.picker = filepicker.New(root, filepicker.PickFile)
		m.picker.SetSize(m.width-6, m.height-6)
		m.pushScreen(m.screen)
		m.screen = screenPicker
	case "open_folder":
		m.picker = filepicker.New(pickerStartPath(""), filepicker.PickDirectory)
		m.picker.SetSize(m.width-6, m.height-6)
		m.pushScreen(m.screen)
		m.screen = screenPicker
	case "new_file":
		m.newFile = newNewFileModel(m.defaultCreateDir())
		m.newFile.SetSize(m.width, m.height)
		m.pushScreen(m.screen)
		m.screen = screenNewFile
	case "close_tab":
		if m.screen == screenEditor && m.hasActiveTab() {
			m.closeActiveTab()
		}
	case "next_tab":
		if m.screen == screenEditor && len(m.tabs) > 1 {
			m.activateTab((m.activeTab + 1) % len(m.tabs))
		}
	case "prev_tab":
		if m.screen == screenEditor && len(m.tabs) > 1 {
			m.activateTab((m.activeTab - 1 + len(m.tabs)) % len(m.tabs))
		}
	case "explain_error":
		if m.screen == screenEditor && m.hasActiveTab() {
			return m.startAIErrorExplanation(m.activeTab)
		}
	case "ai_hint":
		if m.screen == screenEditor && m.hasActiveTab() {
			return m.startAIHint(m.activeTab)
		}
	case "interpreter_picker":
		if m.screen == screenEditor && m.hasActiveTab() && strings.EqualFold(filepath.Ext(m.tabs[m.activeTab].path), ".py") {
			m.interpreterPicker = newInterpreterPickerModel(m.pythonInterpreters, m.tabs[m.activeTab].pythonInterpreter)
			m.interpreterPicker.SetSize(m.width, m.height)
			m.pushScreen(m.screen)
			m.screen = screenInterpreterPicker
		}
	case "model_picker":
		if m.screen == screenEditor {
			m.openModelPicker()
		}
	case "theme_picker":
		if m.screen == screenEditor {
			m.openThemePicker()
		}
	case "quit":
		m.runner.Stop()
		return tea.Quit
	}
	return nil
}

func (m *appModel) startAIErrorExplanation(index int) tea.Cmd {
	if index < 0 || index >= len(m.tabs) || m.aiClient == nil || m.aiClient.Disabled() {
		if index >= 0 && index < len(m.tabs) {
			m.tabs[index].status = "AI decoder requires GEMINI_API_KEY, OPENAI_API_KEY, or ANTHROPIC_API_KEY."
		}
		return nil
	}
	if m.aiLoading {
		m.tabs[index].status = "Another AI request is already in progress."
		return nil
	}

	traceback := strings.TrimSpace(m.tabs[index].output.StderrText())
	if traceback == "" {
		m.tabs[index].status = "No error output to analyze."
		return nil
	}

	m.aiLoading = true
	m.aiThinkingFrame = 0
	m.aiThinkingTab = index
	m.tabs[index].status = "Analyzing error..."
	m.aiRequestSeq++
	m.activeAIRequestID = m.aiRequestSeq
	m.tabs[index].output.StartAIBlock("AI Explanation", "14")
	return m.startAIStream(index, m.activeAIRequestID, "AI Explanation", "14", func(onChunk func(string) error) error {
		_, err := m.aiClient.ExplainError(
			context.Background(),
			m.tabs[index].editor.Content(),
			traceback,
			m.tabs[index].editor.ExecutionLine(),
			onChunk,
		)
		return err
	})
}

func (m *appModel) startAIHint(index int) tea.Cmd {
	if index < 0 || index >= len(m.tabs) {
		return nil
	}
	if m.aiClient == nil || m.aiClient.Disabled() {
		m.tabs[index].status = "AI hints require GEMINI_API_KEY, OPENAI_API_KEY, or ANTHROPIC_API_KEY."
		return nil
	}
	if m.aiLoading {
		m.tabs[index].status = "Another AI request is already in progress."
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
	m.aiLoading = true
	m.aiThinkingFrame = 0
	m.aiThinkingTab = index
	tab.status = fmt.Sprintf("Generating hint (level %d)...", tab.hintLevel)
	m.aiRequestSeq++
	m.activeAIRequestID = m.aiRequestSeq

	currentError := tab.output.StderrText()
	currentLine := tab.editor.ExecutionLine()
	hintLevel := tab.hintLevel
	header := fmt.Sprintf("AI Hint (Level %d)", hintLevel)
	tab.output.StartAIBlock(header, "11")
	return m.startAIStream(index, m.activeAIRequestID, header, "11", func(onChunk func(string) error) error {
		_, err := m.aiClient.GetHint(context.Background(), source, currentError, currentLine, hintLevel, onChunk)
		return err
	})
}

func (m *appModel) startAIStream(index, requestID int, header, headerColor string, run func(func(string) error) error) tea.Cmd {
	go func() {
		err := run(func(chunk string) error {
			m.aiEvents <- aiStreamEvent{
				requestID:   requestID,
				tabIndex:    index,
				chunk:       chunk,
				header:      header,
				headerColor: headerColor,
			}
			return nil
		})
		m.aiEvents <- aiStreamEvent{
			requestID:   requestID,
			tabIndex:    index,
			done:        true,
			err:         err,
			header:      header,
			headerColor: headerColor,
		}
	}()
	return tea.Batch(scheduleAIThinkingTick(), waitForAIStream(m.aiEvents))
}

func formatAIError(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "AI request timed out. Check your connection."
	case errors.Is(err, ai.ErrParseResponse):
		return "AI response could not be parsed."
	default:
		var rateErr ai.ErrRateLimited
		if errors.As(err, &rateErr) {
			return fmt.Sprintf("AI cooldown active. Try again in %s.", rateErr.RetryAfter.Round(time.Second))
		}
		var statusErr ai.ErrHTTPStatus
		if errors.As(err, &statusErr) {
			switch {
			case statusErr.StatusCode == 429 && statusErr.Provider == "gemini":
				return "Gemini rate limit reached. Wait a moment and try again."
			case statusErr.StatusCode == 429 && statusErr.Provider == "openai":
				return "OpenAI rate limit reached. Wait a moment and try again."
			case statusErr.StatusCode == 429 && statusErr.Provider == "anthropic":
				return "Anthropic rate limit reached. Wait a moment and try again."
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

func availableAIModels(cfg appConfig) []aiModelOption {
	var items []aiModelOption
	if strings.TrimSpace(cfg.GeminiKey) != "" {
		items = append(items,
			aiModelOption{provider: "gemini", model: "gemini-2.5-flash", detail: "(recommended, fast)"},
			aiModelOption{provider: "gemini", model: "gemini-2.0-flash", detail: "(stable alternative)"},
			aiModelOption{provider: "gemini", model: "gemini-1.5-flash", detail: "(alternative)"},
			aiModelOption{provider: "gemini", model: "gemini-1.5-pro", detail: "(more capable, lower rate limit)"},
		)
	}
	if strings.TrimSpace(cfg.OpenAIKey) != "" {
		items = append(items,
			aiModelOption{provider: "openai", model: "gpt-4o-mini", detail: "(affordable)"},
			aiModelOption{provider: "openai", model: "gpt-4o", detail: "(most capable)"},
		)
	}
	if strings.TrimSpace(cfg.AnthropicKey) != "" {
		items = append(items,
			aiModelOption{provider: "anthropic", model: "claude-3-5-sonnet-latest", detail: "(teaching-friendly)"},
			aiModelOption{provider: "anthropic", model: "claude-3-5-haiku-latest", detail: "(fast)"},
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

func (m *appModel) contextualFooter() string {
	switch m.screen {
	case screenEditor:
		if !m.hasActiveTab() {
			return "ctrl+s save • ctrl+f find • ctrl+p commands"
		}
		tab := m.tabs[m.activeTab]
		status := strings.ToLower(tab.output.Status())
		if tab.activeRunID == 0 && tab.output.StderrText() != "" && (strings.Contains(status, "finished") || strings.Contains(status, "timed out")) {
			return "ctrl+e explain • ctrl+s save • ctrl+p commands"
		}
		if tab.editor.Dirty() {
			return "ctrl+s save • ctrl+z undo • ctrl+p commands"
		}
		if m.focus == "output" {
			return "ctrl+c copy • ctrl+l input • ctrl+p commands"
		}
		return "ctrl+s save • ctrl+f find • ctrl+p commands"
	case screenWelcome:
		return "enter open • n new file • ctrl+q quit"
	default:
		return "esc back • enter select • ↑↓ navigate"
	}
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
