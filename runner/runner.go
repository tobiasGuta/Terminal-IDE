package runner

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

type Event interface {
	Sequence() int
}

type StartedMsg struct {
	ID int
}

func (m StartedMsg) Sequence() int { return m.ID }

type OutputMsg struct {
	ID    int
	Text  string
	IsErr bool
}

func (m OutputMsg) Sequence() int { return m.ID }

type FinishedMsg struct {
	ID        int
	Err       error
	Cancelled bool
}

func (m FinishedMsg) Sequence() int { return m.ID }

type Manager struct {
	mu     sync.Mutex
	seq    int
	cancel context.CancelFunc
	stdin  io.WriteCloser
	events chan Event
}

func New() *Manager {
	return &Manager{
		events: make(chan Event, 256),
	}
}

func (m *Manager) Events() <-chan Event {
	return m.events
}

func (m *Manager) Start(path, content string) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.cancel != nil {
		m.cancel()
	}
	if m.stdin != nil {
		_ = m.stdin.Close()
		m.stdin = nil
	}

	m.seq++
	id := m.seq
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel

	go m.run(ctx, id, path, content)
	return id
}

func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	if m.stdin != nil {
		_ = m.stdin.Close()
		m.stdin = nil
	}
}

func (m *Manager) SendInput(id int, input string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id != m.seq || m.stdin == nil {
		return fmt.Errorf("no active process is accepting input")
	}
	_, err := io.WriteString(m.stdin, input)
	return err
}

func (m *Manager) run(ctx context.Context, id int, path, content string) {
	m.events <- StartedMsg{ID: id}

	dir, err := os.MkdirTemp("", "terminal-ide-run-*")
	if err != nil {
		m.events <- FinishedMsg{ID: id, Err: err}
		return
	}
	defer os.RemoveAll(dir)

	base := filepath.Base(path)
	if base == "." || base == string(filepath.Separator) || base == "" {
		base = "snippet.py"
	}

	tempPath := filepath.Join(dir, base)
	if err := os.WriteFile(tempPath, []byte(content), 0o644); err != nil {
		m.events <- FinishedMsg{ID: id, Err: err}
		return
	}

	cmd, err := commandForPath(ctx, tempPath)
	if err != nil {
		m.events <- FinishedMsg{ID: id, Err: err}
		return
	}
	cmd.Dir = dir

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		m.events <- FinishedMsg{ID: id, Err: err}
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		m.events <- FinishedMsg{ID: id, Err: err}
		return
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		m.events <- FinishedMsg{ID: id, Err: err}
		return
	}

	if err := cmd.Start(); err != nil {
		m.events <- FinishedMsg{ID: id, Err: err}
		return
	}
	m.setStdin(id, stdin)

	var wg sync.WaitGroup
	wg.Add(2)
	go m.stream(id, stdout, false, &wg)
	go m.stream(id, stderr, true, &wg)
	wg.Wait()
	m.clearStdin(id)

	err = cmd.Wait()
	if ctx.Err() == context.Canceled {
		m.events <- FinishedMsg{ID: id, Cancelled: true}
		return
	}
	m.events <- FinishedMsg{ID: id, Err: err}
}

func (m *Manager) stream(id int, reader io.Reader, isErr bool, wg *sync.WaitGroup) {
	defer wg.Done()

	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		m.events <- OutputMsg{ID: id, Text: scanner.Text() + "\n", IsErr: isErr}
	}

	if err := scanner.Err(); err != nil && !strings.Contains(err.Error(), "file already closed") {
		m.events <- OutputMsg{ID: id, Text: err.Error() + "\n", IsErr: true}
	}
}

func (m *Manager) setStdin(id int, stdin io.WriteCloser) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id == m.seq {
		m.stdin = stdin
		return
	}
	_ = stdin.Close()
}

func (m *Manager) clearStdin(id int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id == m.seq && m.stdin != nil {
		_ = m.stdin.Close()
		m.stdin = nil
	}
}

func commandForPath(ctx context.Context, path string) (*exec.Cmd, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".py":
		interpreter, err := detectPythonInterpreter(ctx, path)
		if err != nil {
			return nil, err
		}
		return exec.CommandContext(ctx, interpreter, path), nil
	case ".go":
		return exec.CommandContext(ctx, "go", "run", path), nil
	default:
		return nil, fmt.Errorf("live run supports .py and .go files right now")
	}
}

func detectPythonInterpreter(ctx context.Context, path string) (string, error) {
	sourceBytes, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	source := string(sourceBytes)

	if explicit := interpreterFromShebang(source); explicit != "" {
		if _, err := exec.LookPath(explicit); err == nil {
			return explicit, nil
		}
	}

	available := availablePythonInterpreters()
	if len(available) == 0 {
		return "", fmt.Errorf("no Python interpreter found; install python3 or python2")
	}

	var py3Candidates []string
	var py2Candidates []string
	for _, interpreter := range available {
		if strings.Contains(interpreter, "python2") {
			py2Candidates = append(py2Candidates, interpreter)
			continue
		}
		if strings.Contains(interpreter, "python3") || interpreter == "python" {
			py3Candidates = append(py3Candidates, interpreter)
		}
	}

	if detected := pickByCompilationProbe(ctx, path, py3Candidates, py2Candidates); detected != "" {
		return detected, nil
	}

	if detected := detectPythonVersionBySyntax(source, py3Candidates, py2Candidates); detected != "" {
		return detected, nil
	}

	if len(py3Candidates) > 0 {
		return py3Candidates[0], nil
	}
	return available[0], nil
}

func availablePythonInterpreters() []string {
	candidates := []string{"python3", "python", "python2"}
	var available []string
	for _, name := range candidates {
		if _, err := exec.LookPath(name); err == nil {
			available = append(available, name)
		}
	}
	return available
}

func interpreterFromShebang(source string) string {
	firstLine, _, _ := strings.Cut(source, "\n")
	if !strings.HasPrefix(firstLine, "#!") {
		return ""
	}

	line := strings.TrimSpace(strings.TrimPrefix(firstLine, "#!"))
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return ""
	}

	if strings.HasSuffix(fields[0], "env") && len(fields) > 1 {
		return fields[1]
	}

	return filepath.Base(fields[0])
}

func pickByCompilationProbe(ctx context.Context, path string, py3Candidates, py2Candidates []string) string {
	py3 := firstCompilingInterpreter(ctx, path, py3Candidates)
	py2 := firstCompilingInterpreter(ctx, path, py2Candidates)

	switch {
	case py3 != "" && py2 == "":
		return py3
	case py2 != "" && py3 == "":
		return py2
	case py3 != "" && py2 != "":
		return py3
	default:
		return ""
	}
}

func firstCompilingInterpreter(ctx context.Context, path string, interpreters []string) string {
	for _, interpreter := range interpreters {
		if pythonCompiles(ctx, interpreter, path) {
			return interpreter
		}
	}
	return ""
}

func pythonCompiles(ctx context.Context, interpreter, path string) bool {
	cmd := exec.CommandContext(ctx, interpreter, "-m", "py_compile", path)
	err := cmd.Run()
	return err == nil
}

func detectPythonVersionBySyntax(source string, py3Candidates, py2Candidates []string) string {
	if prefersPython2(source) && len(py2Candidates) > 0 {
		return py2Candidates[0]
	}
	if prefersPython3(source) && len(py3Candidates) > 0 {
		return py3Candidates[0]
	}
	if len(py3Candidates) > 0 {
		return py3Candidates[0]
	}
	if len(py2Candidates) > 0 {
		return py2Candidates[0]
	}
	return ""
}

func prefersPython2(source string) bool {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?m)^\s*print\s+[^(\n]`),
		regexp.MustCompile(`\bxrange\s*\(`),
		regexp.MustCompile(`\braw_input\s*\(`),
		regexp.MustCompile(`\biteritems\s*\(`),
		regexp.MustCompile(`\bbasestring\b`),
		regexp.MustCompile(`\blong\b`),
		regexp.MustCompile(`except\s+[^:\n]+,\s*[A-Za-z_][A-Za-z0-9_]*\s*:`),
	}
	return matchesAny(source, patterns)
}

func prefersPython3(source string) bool {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?m)^\s*async\s+def\s+`),
		regexp.MustCompile(`(?m)^\s*await\s+`),
		regexp.MustCompile(`\bnonlocal\b`),
		regexp.MustCompile(`f["']`),
		regexp.MustCompile(`\bprint\s*\(`),
	}
	return matchesAny(source, patterns)
}

func matchesAny(source string, patterns []*regexp.Regexp) bool {
	for _, pattern := range patterns {
		if pattern.MatchString(source) {
			return true
		}
	}
	return false
}
