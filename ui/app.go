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
	runner            *runner.Manager
	status            string
	debounceToken     int
	activeRunID       int
	pythonInterpreter string
	hintLevel         int
	lastHintSource    string
}

type editorLayout struct {
	panelX       int
	panelY       int
	panelWidth   int
	topWidth     int
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
	return m.welcome.Init()
}

func (m *appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resize()
		return m, nil

	case runnerEventMsg:
		return m, m.handleRunnerEvent(msg)

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
				m.tabs[m.activeTab].runner.StopRun(m.tabs[m.activeTab].activeRunID)
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
			return m, m.startRunForTab(idx, "Queued run...")
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
				if err := m.tabs[m.activeTab].runner.SendInput(m.tabs[m.activeTab].activeRunID, submitted+"\n"); err != nil {
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

	if m.screen == screenEditor && m.hasActiveTab() && m.focus == "output" && !m.tabs[m.activeTab].output.InputFocused() {
		switch key.String() {
		case "left", "alt+left":
			m.tabs[m.activeTab].output.ScrollHorizontal(-4)
			return m, nil
		case "right", "alt+right":
			m.tabs[m.activeTab].output.ScrollHorizontal(4)
			return m, nil
		case "home":
			m.tabs[m.activeTab].output.ScrollHorizontal(-1 << 20)
			return m, nil
		}
	}

	if m.screen == screenEditor && m.hasActiveTab() && m.focus == "editor" && m.prompt.mode == promptNone {
		switch key.String() {
		case "alt+left":
			m.tabs[m.activeTab].editor.ScrollHorizontal(-4)
			return m, nil
		case "alt+right":
			m.tabs[m.activeTab].editor.ScrollHorizontal(4)
			return m, nil
		case "alt+home":
			m.tabs[m.activeTab].editor.ScrollHorizontal(-1 << 20)
			return m, nil
		}
	}

	switch key.String() {
	case "ctrl+q":
		m.stopAllRuns()
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
			m.stopAllRuns()
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
			return m, m.saveActiveTab()
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
			return m, m.startRunForTab(m.activeTab, "Interpreter changed, running...")
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
		runner: runner.New(),
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
		m.tabs[m.activeTab].runner.StopRun(m.tabs[m.activeTab].activeRunID)
	}
	m.tabs[m.activeTab].runner.Stop()

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

func (m *appModel) startRunForTab(index int, status string) tea.Cmd {
	if index < 0 || index >= len(m.tabs) {
		return nil
	}
	tab := &m.tabs[index]
	opts := runner.RunOptions{
		MaxRuntime: runner.DefaultMaxRuntime,
	}
	if strings.EqualFold(filepath.Ext(tab.path), ".py") {
		opts.PythonInterpreter = tab.pythonInterpreter
	}
	tab.activeRunID = tab.runner.Start(tab.path, tab.editor.Content(), opts)
	tab.editor.ClearExecution()
	tab.output.StartSession(status, m.runCommandLabel(*tab))
	tab.output.ClearInput()
	tab.output.SetInputFocus(false)
	tab.status = status
	return waitForRunnerEvent(tab.id, tab.runner)
}

func (m *appModel) bumpDebounce(index int) int {
	if index < 0 || index >= len(m.tabs) {
		return 0
	}
	m.tabs[index].debounceToken++
	return m.tabs[index].debounceToken
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
