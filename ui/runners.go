package ui

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"terminal-ide/runner"
)

type runnerEventMsg struct {
	tabID int
	event runner.Event
}

type installRunnerEventMsg struct {
	tabID int
	event runner.Event
}

var missingModulePattern = regexp.MustCompile(`ModuleNotFoundError:\s+No module named ['"]([^'"]+)['"]`)

func waitForRunnerEvent(tabID int, manager *runner.Manager) tea.Cmd {
	return func() tea.Msg {
		return runnerEventMsg{tabID: tabID, event: <-manager.Events()}
	}
}

func waitForInstallEvent(tabID int, manager *runner.Manager) tea.Cmd {
	return func() tea.Msg {
		return installRunnerEventMsg{tabID: tabID, event: <-manager.Events()}
	}
}

func (m *appModel) handleRunnerEvent(msg runnerEventMsg) tea.Cmd {
	idx := m.findTabByID(msg.tabID)
	if idx < 0 {
		return nil
	}

	tab := &m.tabs[idx]
	switch event := msg.event.(type) {
	case runner.StartedMsg:
		if event.ID == tab.activeRunID {
			tab.output.SetStatus("Running...")
			tab.status = "Running..."
			tab.output.ClearMissingModulePrompt()
			tab.missingModuleDismissed = false
		}
	case runner.OutputMsg:
		if event.ID == tab.activeRunID {
			tab.output.Append(event.Text, event.IsErr)
			if event.IsErr && !tab.missingModuleDismissed {
				if matches := missingModulePattern.FindStringSubmatch(tab.output.StderrText()); len(matches) == 2 {
					tab.output.SetMissingModulePrompt(matches[1])
				}
			}
		}
	case runner.ExecutionMsg:
		if event.ID == tab.activeRunID {
			tab.editor.SetExecution(event.Line, event.Waiting)
			switch event.State {
			case "waiting_input":
				tab.status = fmt.Sprintf("Waiting for input on line %d", event.Line)
			case "line", "resumed":
				tab.status = fmt.Sprintf("Executing line %d", event.Line)
			case "finished":
				tab.status = fmt.Sprintf("Finished on line %d", event.Line)
			case "exception":
				tab.status = fmt.Sprintf("Stopped on line %d", event.Line)
			}
		}
	case runner.FinishedMsg:
		if event.ID == tab.activeRunID {
			tab.activeRunID = 0
			if idx == m.activeTab {
				tab.output.SetInputFocus(false)
				m.focus = "editor"
			}
			finalLine := tab.editor.ExecutionLine()
			switch {
			case event.Cancelled:
				tab.editor.ClearExecution()
				tab.output.SetStatus("Run cancelled")
				tab.status = "Run cancelled"
			case event.TimedOut:
				tab.editor.ClearExecution()
				tab.output.SetStatus("Run timed out")
				if event.Err != nil {
					tab.output.Append(event.Err.Error(), true)
				}
				tab.status = "Run timed out"
			case event.Err != nil:
				tab.output.SetStatus("Run finished with errors")
				tab.output.Append(event.Err.Error(), true)
				if finalLine > 0 {
					tab.status = fmt.Sprintf("Run stopped on line %d", finalLine)
				} else {
					tab.status = "Run finished with errors"
				}
			default:
				tab.output.SetStatus("Run finished successfully")
				if finalLine > 0 {
					tab.status = fmt.Sprintf("Run finished on line %d", finalLine)
				} else {
					tab.status = "Run finished successfully"
				}
			}
		}
	}

	return waitForRunnerEvent(msg.tabID, tab.runner)
}

func (m *appModel) handleInstallRunnerEvent(msg installRunnerEventMsg) tea.Cmd {
	if m.installRunner == nil {
		return nil
	}
	idx := m.findTabByID(msg.tabID)
	if idx < 0 {
		m.clearInstallState()
		return nil
	}

	tab := &m.tabs[idx]
	switch event := msg.event.(type) {
	case runner.StartedMsg:
		if event.ID == m.installRunID {
			tab.output.SetStatus("Installing...")
			tab.status = "Installing..."
		}
	case runner.OutputMsg:
		if event.ID == m.installRunID {
			tab.output.Append(event.Text, event.IsErr)
		}
	case runner.FinishedMsg:
		if event.ID != m.installRunID {
			return waitForInstallEvent(msg.tabID, m.installRunner)
		}
		m.installRunID = 0
		switch {
		case event.Cancelled:
			tab.output.SetStatus("Error")
			tab.status = "Install cancelled"
			m.clearInstallState()
			return nil
		case event.TimedOut:
			tab.output.SetStatus("Error")
			tab.status = "Install timed out"
			if event.Err != nil {
				tab.output.Append(event.Err.Error(), true)
			}
			m.clearInstallState()
			return nil
		case event.Err != nil:
			tab.output.SetStatus("Error")
			tab.status = "Install failed"
			tab.output.Append(event.Err.Error(), true)
			m.clearInstallState()
			return nil
		default:
			if len(m.installQueue) > 0 {
				return m.startNextInstallCommand()
			}
			tab.output.SetStatus("Done")
			tab.status = "Install done"
			if m.installVenvPath != "" {
				m.activeVenvPath = m.installVenvPath
			}
			m.clearInstallState()
			return nil
		}
	}

	return waitForInstallEvent(msg.tabID, m.installRunner)
}

func (m *appModel) stopAllRuns() {
	for i := range m.tabs {
		if m.tabs[i].runner != nil {
			m.tabs[i].runner.Stop()
		}
	}
	if m.installRunner != nil {
		m.installRunner.Stop()
	}
	m.clearInstallState()
}

func (m *appModel) runCommandLabel(tab editorTab) string {
	base := filepath.Base(tab.path)
	switch strings.ToLower(filepath.Ext(tab.path)) {
	case ".py":
		interpreter := "python"
		if m.activeVenvPath != "" {
			interpreter = filepath.Base(filepath.Join(m.activeVenvPath, "bin", "python"))
		} else if tab.pythonInterpreter != "" {
			interpreter = filepath.Base(tab.pythonInterpreter)
		}
		return "➜ " + interpreter + " " + base
	case ".go":
		return "➜ go run " + base
	default:
		return "➜ " + base
	}
}
