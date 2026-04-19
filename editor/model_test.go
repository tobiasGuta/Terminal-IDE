package editor

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestCursorMovementAcrossLines(t *testing.T) {
	m := New()
	m.LoadFile("sample.py", "abc\ndef")
	m.SetSize(80, 20)

	m.cursorRow = 0
	m.cursorCol = 3

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if updated.cursorRow != 1 || updated.cursorCol != 0 {
		t.Fatalf("expected cursor to move to next line start, got row=%d col=%d", updated.cursorRow, updated.cursorCol)
	}

	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if updated.cursorRow != 0 || updated.cursorCol != 3 {
		t.Fatalf("expected cursor to move back to previous line end, got row=%d col=%d", updated.cursorRow, updated.cursorCol)
	}
}

func TestSelectionReturnsSelectedText(t *testing.T) {
	m := New()
	m.LoadFile("sample.py", "alpha\nbeta")
	m.SetSize(80, 20)

	m.selStartRow = 0
	m.selStartCol = 1
	m.selEndRow = 1
	m.selEndCol = 2

	if got := m.SelectedText(); got != "lpha\nbe" {
		t.Fatalf("SelectedText() = %q, want %q", got, "lpha\nbe")
	}
}

func TestInsertNewlineKeepsIndentation(t *testing.T) {
	m := New()
	m.LoadFile("sample.py", "if True:")
	m.SetSize(80, 20)
	m.cursorRow = 0
	m.cursorCol = len("if True:")

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if len(updated.lines) != 2 {
		t.Fatalf("expected 2 lines after enter, got %d", len(updated.lines))
	}
	if updated.lines[1] != "    " {
		t.Fatalf("expected indented new line, got %q", updated.lines[1])
	}
	if updated.cursorRow != 1 || updated.cursorCol != 4 {
		t.Fatalf("expected cursor at indented position, got row=%d col=%d", updated.cursorRow, updated.cursorCol)
	}
}

func TestAutoCloseBracketAndBackspacePair(t *testing.T) {
	m := New()
	m.LoadFile("sample.py", "")
	m.SetSize(80, 20)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'('}})
	if got := updated.Content(); got != "()" {
		t.Fatalf("expected auto-closed parens, got %q", got)
	}
	if updated.cursorCol != 1 {
		t.Fatalf("expected cursor inside parens, got %d", updated.cursorCol)
	}

	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if got := updated.Content(); got != "" {
		t.Fatalf("expected backspace to remove pair, got %q", got)
	}
}

func TestRedoTraversesFullHistory(t *testing.T) {
	m := New()
	m.LoadFile("sample.py", "")
	m.SetSize(80, 20)

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if got := updated.Content(); got != "abc" {
		t.Fatalf("expected content abc, got %q", got)
	}

	if !updated.Undo() || updated.Content() != "ab" {
		t.Fatalf("expected first undo to produce ab, got %q", updated.Content())
	}
	if !updated.Undo() || updated.Content() != "a" {
		t.Fatalf("expected second undo to produce a, got %q", updated.Content())
	}

	if !updated.Redo() || updated.Content() != "ab" {
		t.Fatalf("expected first redo to produce ab, got %q", updated.Content())
	}
	if !updated.Redo() || updated.Content() != "abc" {
		t.Fatalf("expected second redo to produce abc, got %q", updated.Content())
	}
	if updated.Redo() {
		t.Fatalf("expected redo stack to be exhausted")
	}
}

func TestFindNextHighlightsMatchedText(t *testing.T) {
	m := New()
	m.LoadFile("sample.py", "alpha beta alpha")
	m.SetSize(80, 20)

	found, _ := m.FindNext("beta", true)
	if !found {
		t.Fatalf("expected to find beta")
	}
	if got := m.SelectedText(); got != "beta" {
		t.Fatalf("expected selected match beta, got %q", got)
	}
	start, end, ok := m.selectionForLine(0)
	if !ok {
		t.Fatalf("expected selection highlight on current line")
	}
	if start != 6 || end != 10 {
		t.Fatalf("expected selection range 6..10, got %d..%d", start, end)
	}
}

func TestBackspaceDoesNotDeleteNonEmptyPair(t *testing.T) {
	m := New()
	m.LoadFile("sample.py", "(a)")
	m.SetSize(80, 20)
	m.cursorRow = 0
	m.cursorCol = 2

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if got := updated.Content(); got != "()" {
		t.Fatalf("expected only inner character to be deleted, got %q", got)
	}
	if updated.cursorCol != 1 {
		t.Fatalf("expected cursor to move back by one, got %d", updated.cursorCol)
	}
}

func TestFindNextReportsWrap(t *testing.T) {
	m := New()
	m.LoadFile("sample.py", "alpha\nbeta\nalpha")
	m.SetSize(80, 20)
	m.cursorRow = 2
	m.cursorCol = len("alpha")

	found, wrapped := m.FindNext("beta", true)
	if !found {
		t.Fatalf("expected to find beta after wrapping")
	}
	if !wrapped {
		t.Fatalf("expected wrapped search to report wrap")
	}
	if got := m.SelectedText(); got != "beta" {
		t.Fatalf("expected beta to be selected after wrap, got %q", got)
	}
}
