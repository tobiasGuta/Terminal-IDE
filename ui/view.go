package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

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
		maxEditorContentWidth := max(131, int(float64(panelWidth)*0.77))
		availableEditorOuterWidth := max(20, panelWidth-sidebarOuterWidth-panelStyle.GetHorizontalFrameSize())
		editorOuterWidth := min(availableEditorOuterWidth, maxEditorContentWidth+activePanelStyle.GetHorizontalFrameSize())
		sidebarContentWidth := max(10, sidebarOuterWidth-panelStyle.GetHorizontalFrameSize())
		editorContentWidth := max(10, editorOuterWidth-activePanelStyle.GetHorizontalFrameSize())
		topBodyHeight := lipgloss.Height("x") + lipgloss.Height("x") + editorBodyHeight
		tab.editor.SetSize(editorContentWidth, editorBodyHeight)
		tab.output.SetSize(max(10, panelWidth-4), max(2, m.outputHeight-2))
		if venv := m.installVenvLabel(); venv != "" {
			tab.output.SetHeaderNote("venv: " + venv)
		} else {
			tab.output.SetHeaderNote("")
		}
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
			topWidth:     lipgloss.Width(top),
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
		display := m.tabDisplayLabel(i, item.label)
		if i == m.activeTab {
			rendered = append(rendered, accentStyle.Render(display))
		} else {
			rendered = append(rendered, mutedStyle.Render(display))
		}
	}
	if end < len(items) {
		rendered = append(rendered, mutedStyle.Render("›"))
	}
	return strings.Join(rendered, "  ")
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
		itemWidth := lipgloss.Width(m.tabDisplayLabel(items[end].index, items[end].label)) + 2
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
			used -= lipgloss.Width(m.tabDisplayLabel(items[end].index, items[end].label)) + 4
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

func (m *appModel) tabDisplayLabel(index int, label string) string {
	if index == m.activeTab {
		return "[" + label + "]"
	}
	return label
}

func (m *appModel) renderInterpreterBadge(tab editorTab) string {
	if !strings.EqualFold(filepath.Ext(tab.path), ".py") {
		return ""
	}

	label := "auto"
	if m.activeVenvPath != "" {
		label = filepath.Base(m.activeVenvPath)
	} else if tab.pythonInterpreter != "" {
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
	maxEditorContentWidth := max(131, int(float64(panelWidth)*0.77))
	availableEditorOuterWidth := max(20, panelWidth-sidebarOuterWidth-panelStyle.GetHorizontalFrameSize())
	editorOuterWidth := min(availableEditorOuterWidth, maxEditorContentWidth+activePanelStyle.GetHorizontalFrameSize())
	editorContentWidth := max(10, editorOuterWidth-activePanelStyle.GetHorizontalFrameSize())
	return editorLayout{
		panelX:       appPaddingStyle.GetHorizontalFrameSize() / 2,
		panelY:       appPaddingStyle.GetVerticalFrameSize() / 2,
		panelWidth:   panelWidth,
		topWidth:     sidebarOuterWidth + 1 + editorOuterWidth,
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
