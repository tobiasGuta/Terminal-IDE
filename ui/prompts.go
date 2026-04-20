package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

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
		m.refreshPromptPreview()
		return true, nil
	case "shift+tab":
		if len(m.prompt.fields) > 0 {
			m.prompt.activeField = (m.prompt.activeField - 1 + len(m.prompt.fields)) % len(m.prompt.fields)
		}
		m.refreshPromptPreview()
		return true, nil
	case "backspace":
		field := &m.prompt.fields[m.prompt.activeField]
		if len(field.value) > 0 {
			field.value = field.value[:len(field.value)-1]
		}
		m.refreshPromptPreview()
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
		m.refreshPromptPreview()
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
			if found, wrapped := m.tabs[m.activeTab].editor.FindNext(find, true); found {
				m.tabs[m.activeTab].status = formatFindStatus(find, true, wrapped, "Replaced %q and moved to next match", "Replaced %q and wrapped to next match")
			} else {
				m.tabs[m.activeTab].status = fmt.Sprintf("Replaced final %q", find)
			}
			return cmd
		}
		if found, wrapped := m.tabs[m.activeTab].editor.FindNext(find, forward); found {
			m.tabs[m.activeTab].status = formatFindStatus(find, forward, wrapped, "", "")
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
	case promptInstallPackage:
		pkg := strings.TrimSpace(string(m.prompt.fields[0].value))
		if pkg == "" {
			m.tabs[m.activeTab].status = "Enter a package name"
			return nil
		}
		m.installPackage = pkg
		m.openInstallVenvPrompt()
		return nil
	case promptInstallVenvConfirm:
		answer := strings.ToLower(strings.TrimSpace(string(m.prompt.fields[0].value)))
		if answer == "y" || answer == "yes" {
			m.openInstallVenvNamePrompt()
			return nil
		}
		m.prompt = inlinePrompt{}
		return m.startPackageInstall(m.activeTab, m.installPackage, false, "")
	case promptInstallVenvName:
		envName := strings.TrimSpace(string(m.prompt.fields[0].value))
		if envName == "" {
			envName = ".venv"
		}
		m.prompt = inlinePrompt{}
		return m.startPackageInstall(m.activeTab, m.installPackage, true, envName)
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

func (m *appModel) refreshPromptPreview() {
	if m.prompt.mode != promptFind || !m.hasActiveTab() || m.prompt.activeField != 0 || len(m.prompt.fields) == 0 {
		return
	}
	find := strings.TrimSpace(string(m.prompt.fields[0].value))
	if find == "" {
		m.tabs[m.activeTab].status = "Type to preview matches"
		return
	}
	if found, wrapped := m.tabs[m.activeTab].editor.PreviewFind(find); found {
		m.tabs[m.activeTab].status = formatFindStatus(find, true, wrapped, "Previewing %q", "Previewing %q (wrapped)")
		return
	}
	m.tabs[m.activeTab].status = fmt.Sprintf("No matches for %q", find)
}

func formatFindStatus(find string, forward, wrapped bool, forwardFormat, wrappedForwardFormat string) string {
	if forwardFormat == "" {
		forwardFormat = "Found next %q"
	}
	if wrappedForwardFormat == "" {
		wrappedForwardFormat = "Found next %q (wrapped)"
	}
	if forward {
		if wrapped {
			return fmt.Sprintf(wrappedForwardFormat, find)
		}
		return fmt.Sprintf(forwardFormat, find)
	}
	if wrapped {
		return fmt.Sprintf("Found previous %q (wrapped)", find)
	}
	return fmt.Sprintf("Found previous %q", find)
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
