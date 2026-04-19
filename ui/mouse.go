package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type tabHitbox struct {
	index int
	start int
	end   int
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
		width := lipgloss.Width(m.tabDisplayLabel(item.index, item.label)) + 3
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

	if y >= layout.panelY && y < layout.panelY+layout.topHeight && x >= layout.panelX && x < layout.panelX+layout.topWidth {
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
			viewRow := y - layout.editorBodyY
			viewCol := x - layout.editorX
			m.tabs[m.activeTab].editor.SetCursorFromView(viewRow, viewCol)
			m.tabs[m.activeTab].editor.BeginSelectionFromView(viewRow, viewCol)
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
	if y >= layout.panelY && y < layout.panelY+layout.topHeight && x >= layout.panelX && x < layout.panelX+layout.topWidth {
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
	if y >= layout.panelY && y < layout.panelY+layout.topHeight && x >= layout.panelX && x < layout.panelX+layout.topWidth {
		m.tabs[m.activeTab].editor.Scroll(delta)
	}
}
