package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"terminal-ide/filepicker"
)

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

func (m *appModel) saveActiveTab() tea.Cmd {
	if m.screen != screenEditor || !m.hasActiveTab() {
		return nil
	}
	tab := &m.tabs[m.activeTab]
	if err := os.WriteFile(tab.path, []byte(tab.editor.Content()), 0o644); err != nil {
		tab.status = err.Error()
		return nil
	}
	tab.editor.MarkSaved()
	m.refreshWorkspaceView()
	tab.status = fmt.Sprintf("Saved %s", filepath.Base(tab.path))
	return m.startRunForTab(m.activeTab, "Saved and running...")
}

func (m *appModel) executeCommandAction(action string) tea.Cmd {
	switch action {
	case "save":
		return m.saveActiveTab()
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
	case "install_package":
		if m.screen == screenEditor && m.hasActiveTab() {
			m.openInstallPackagePrompt("")
		} else {
			m.status = "Open a file tab to install packages for that workspace."
		}
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
		m.stopAllRuns()
		return tea.Quit
	case "noop":
		return nil
	}
	return nil
}
