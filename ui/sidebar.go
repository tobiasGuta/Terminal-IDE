package ui

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type sidebarEntry struct {
	label string
	path  string
	depth int
	isDir bool
}

func (m *appModel) refreshWorkspaceView() {
	if m.workspaceRoot == "" {
		if m.hasActiveTab() {
			m.workspaceRoot = filepath.Dir(m.tabs[m.activeTab].path)
		} else {
			m.sidebarEntries = nil
			m.gitStatus = map[string]string{}
			return
		}
	}
	m.sidebarEntries = buildSidebarEntries(m.workspaceRoot)
	m.gitStatus = loadGitStatus(m.workspaceRoot)
}

func buildSidebarEntries(root string) []sidebarEntry {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}
	root = filepath.Clean(root)

	var entries []sidebarEntry
	entries = append(entries, sidebarEntry{
		label: filepath.Base(root),
		path:  root,
		isDir: true,
	})

	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == root {
			return nil
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") && d.IsDir() {
			return filepath.SkipDir
		}
		if strings.HasPrefix(name, ".") && !d.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		depth := strings.Count(rel, string(filepath.Separator)) + 1
		entries = append(entries, sidebarEntry{
			label: name,
			path:  path,
			depth: depth,
			isDir: d.IsDir(),
		})
		return nil
	})

	sort.SliceStable(entries[1:], func(i, j int) bool {
		left := entries[i+1]
		right := entries[j+1]
		if left.depth != right.depth {
			return left.path < right.path
		}
		if left.isDir != right.isDir {
			return left.isDir
		}
		return left.path < right.path
	})
	return entries
}

func (m *appModel) visibleSidebarEntries(height int) []sidebarEntry {
	if height <= 0 || len(m.sidebarEntries) == 0 {
		return nil
	}
	if m.sidebarScroll < 0 {
		m.sidebarScroll = 0
	}
	if m.sidebarScroll >= len(m.sidebarEntries) {
		m.sidebarScroll = max(0, len(m.sidebarEntries)-1)
	}
	end := min(len(m.sidebarEntries), m.sidebarScroll+height)
	return m.sidebarEntries[m.sidebarScroll:end]
}

func (m *appModel) ensureSidebarEntryVisible(path string, height int) {
	if path == "" || height <= 0 {
		return
	}
	for i, entry := range m.sidebarEntries {
		if entry.path != path {
			continue
		}
		if i < m.sidebarScroll {
			m.sidebarScroll = i
		}
		if i >= m.sidebarScroll+height {
			m.sidebarScroll = i - height + 1
		}
		return
	}
}

func (m *appModel) renderSidebar(width, height int) string {
	if width < 12 {
		return ""
	}
	bodyHeight := max(1, height-2)
	entries := m.visibleSidebarEntries(bodyHeight)
	lines := []string{accentStyle.Render("Workspace")}
	if m.workspaceRoot != "" {
		lines = append(lines, mutedStyle.Render(truncateLabel(m.workspaceRoot, max(1, width-2))))
	}
	if len(entries) == 0 {
		lines = append(lines, mutedStyle.Render("No files"))
		return strings.Join(lines, "\n")
	}

	for _, entry := range entries {
		prefix := strings.Repeat("  ", entry.depth)
		icon := "· "
		if entry.isDir {
			icon = "▸ "
		}
		name := entry.label
		if status := m.gitStatus[entry.path]; status != "" {
			name = status + " " + name
		}
		style := lipgloss.NewStyle()
		if entry.isDir {
			style = mutedStyle
		}
		if m.hasActiveTab() && entry.path == m.tabs[m.activeTab].path {
			style = accentStyle
		}
		lines = append(lines, style.Render(truncateLabel(prefix+icon+name, max(1, width-2))))
	}

	return strings.Join(lines, "\n")
}
