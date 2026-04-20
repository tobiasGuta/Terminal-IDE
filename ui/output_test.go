package ui

import (
	"fmt"
	"strings"
	"testing"
)

func TestOutputModelTrimsScrollback(t *testing.T) {
	m := newOutputModel()
	for i := 0; i < maxOutputScrollbackLines+25; i++ {
		m.Append(fmt.Sprintf("line-%d\n", i), false)
	}

	if len(m.lines) != maxOutputScrollbackLines {
		t.Fatalf("expected %d stored lines, got %d", maxOutputScrollbackLines, len(m.lines))
	}
	if got := m.lines[0].text; got != "line-25" {
		t.Fatalf("expected first retained line to be line-25, got %q", got)
	}
	if got := m.lines[len(m.lines)-1].text; got != fmt.Sprintf("line-%d", maxOutputScrollbackLines+24) {
		t.Fatalf("unexpected last retained line: %q", got)
	}
}

func TestOutputModelAppendsAIChunksProgressively(t *testing.T) {
	m := newOutputModel()
	m.StartAIBlock("AI Explanation", "14")
	m.AppendAIChunk("hello")
	m.AppendAIChunk(" world\nnext")

	if !m.aiStreaming {
		t.Fatalf("expected AI block to remain in streaming mode until finished")
	}
	if len(m.lines) < 3 {
		t.Fatalf("expected AI header plus streamed lines, got %d lines", len(m.lines))
	}
	if got := m.lines[1].text; got != "hello world" {
		t.Fatalf("expected first AI content line to update incrementally, got %q", got)
	}
	if got := m.lines[2].text; got != "next" {
		t.Fatalf("expected second AI content line, got %q", got)
	}
}

func TestAppUpdateStreamsAIChunksIntoOutput(t *testing.T) {
	m := NewApp().(*appModel)
	m.width = 120
	m.height = 40
	m.resize()

	path := writeTempFile(t, t.TempDir(), "stream.py", "print('hi')\n")
	idx, err := m.openFile(path)
	if err != nil {
		t.Fatalf("openFile failed: %v", err)
	}

	m.activeAIRequestID = 7
	m.aiLoading = true
	m.tabs[idx].output.StartAIBlock("AI Explanation", "14")

	model, _ := m.Update(aiStreamMsg{event: aiStreamEvent{
		requestID: 7,
		tabIndex:  idx,
		chunk:     "partial",
	}})
	app := model.(*appModel)
	if got := app.tabs[idx].output.lines[1].text; got != "partial" {
		t.Fatalf("expected chunk to render immediately, got %q", got)
	}
	if app.tabs[idx].status != "Streaming AI response..." {
		t.Fatalf("expected streaming status, got %q", app.tabs[idx].status)
	}

	model, _ = app.Update(aiStreamMsg{event: aiStreamEvent{
		requestID: 7,
		tabIndex:  idx,
		done:      true,
	}})
	app = model.(*appModel)
	if app.aiLoading {
		t.Fatalf("expected aiLoading to stop after done event")
	}
	if app.tabs[idx].status != "AI response ready" {
		t.Fatalf("expected final status, got %q", app.tabs[idx].status)
	}
	if app.tabs[idx].output.aiStreaming {
		t.Fatalf("expected AI block to be finalized")
	}
}

func TestOutputModelStderrTextIncludesPendingError(t *testing.T) {
	m := newOutputModel()
	m.Append("boom", true)
	if got := strings.TrimSpace(m.StderrText()); got != "boom" {
		t.Fatalf("expected pending stderr to be included, got %q", got)
	}
}

func TestOutputModelHorizontalScrollChangesVisibleSlice(t *testing.T) {
	m := newOutputModel()
	m.SetSize(8, 5)
	m.Append("abcdefghijklmnop\n", false)

	if got := m.renderOutputLine(0, m.lines[0]); got != "abcdefgh" {
		t.Fatalf("expected initial visible slice, got %q", got)
	}
	m.ScrollHorizontal(4)
	if got := m.renderOutputLine(0, m.lines[0]); got != "efghijkl" {
		t.Fatalf("expected scrolled visible slice, got %q", got)
	}
}

func TestOutputModelRendersMissingModulePrompt(t *testing.T) {
	m := newOutputModel()
	m.SetSize(80, 6)
	m.SetMissingModulePrompt("requests")
	view := m.View()
	if !strings.Contains(view, "Missing module 'requests'") {
		t.Fatalf("expected missing-module prompt, got %q", view)
	}
}
