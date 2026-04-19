package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestCommandPaletteFindActionOpensPrompt(t *testing.T) {
	m := NewApp().(*appModel)
	m.width = 120
	m.height = 40
	m.resize()

	path := writeTempFile(t, t.TempDir(), "sample.py", "print('hi')\n")
	if _, err := m.openFile(path); err != nil {
		t.Fatalf("openFile failed: %v", err)
	}

	if cmd := m.executeCommandAction("find"); cmd != nil {
		t.Fatalf("expected find action not to return a command")
	}
	if m.prompt.mode != promptFind {
		t.Fatalf("expected prompt mode %v, got %v", promptFind, m.prompt.mode)
	}
}

func TestOpenFileCreatesPerTabRunner(t *testing.T) {
	m := NewApp().(*appModel)
	m.width = 120
	m.height = 40
	m.resize()

	dir := t.TempDir()
	first := writeTempFile(t, dir, "one.py", "print('one')\n")
	second := writeTempFile(t, dir, "two.py", "print('two')\n")
	firstIdx, err := m.openFile(first)
	if err != nil {
		t.Fatalf("openFile first failed: %v", err)
	}
	secondIdx, err := m.openFile(second)
	if err != nil {
		t.Fatalf("openFile second failed: %v", err)
	}

	if m.tabs[firstIdx].runner == nil || m.tabs[secondIdx].runner == nil {
		t.Fatalf("expected each tab to have a runner")
	}
	if m.tabs[firstIdx].runner == m.tabs[secondIdx].runner {
		t.Fatalf("expected tabs to use distinct runner managers")
	}
}

func TestRenderTabBarKeepsOverflowBounded(t *testing.T) {
	m := NewApp().(*appModel)
	m.width = 120
	m.height = 40
	m.resize()

	dir := t.TempDir()
	for i := 0; i < 8; i++ {
		name := filepath.Join(dir, strings.Repeat("verylongtab", 2)+string(rune('a'+i))+".py")
		if err := os.WriteFile(name, []byte("print('x')\n"), 0o644); err != nil {
			t.Fatalf("write file %d failed: %v", i, err)
		}
		if _, err := m.openFile(name); err != nil {
			t.Fatalf("openFile %d failed: %v", i, err)
		}
	}

	m.activateTab(len(m.tabs) - 1)
	bar := m.renderTabBar(38)
	if lipgloss.Width(bar) > 38 {
		t.Fatalf("expected tab bar width <= 38, got %d", lipgloss.Width(bar))
	}
	if !strings.Contains(bar, "‹") {
		t.Fatalf("expected overflow indicator in tab bar, got %q", bar)
	}
}

func TestMouseClickMovesEditorCursor(t *testing.T) {
	m := NewApp().(*appModel)
	m.width = 120
	m.height = 40
	m.resize()

	path := writeTempFile(t, t.TempDir(), "cursor.py", "first\nsecond\nthird\n")
	if _, err := m.openFile(path); err != nil {
		t.Fatalf("openFile failed: %v", err)
	}

	_ = m.View()
	if line := m.tabs[m.activeTab].editor.CurrentCursorLine(); line != 1 {
		t.Fatalf("expected initial cursor line 1, got %d", line)
	}

	m.handleMouseClick(m.layout.editorX+6, m.layout.editorBodyY+1)
	if line := m.tabs[m.activeTab].editor.CurrentCursorLine(); line != 2 {
		t.Fatalf("expected cursor line 2 after click, got %d", line)
	}
}

func writeTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeTempFile failed: %v", err)
	}
	return path
}
