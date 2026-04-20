package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
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

type ExecutionMsg struct {
	ID      int
	File    string
	Line    int
	State   string
	Waiting bool
}

func (m ExecutionMsg) Sequence() int { return m.ID }

type FinishedMsg struct {
	ID        int
	Err       error
	Cancelled bool
	TimedOut  bool
}

func (m FinishedMsg) Sequence() int { return m.ID }

type Manager struct {
	mu     sync.Mutex
	seq    int
	cancel context.CancelFunc
	stdin  io.WriteCloser
	events chan Event
}

const executionEventPrefix = "__TUI_EVT__"

type RunOptions struct {
	PythonInterpreter string
	MaxRuntime        time.Duration
	DisableSandbox    bool
}

type PythonInterpreter struct {
	Command string
	Path    string
	Version string
	Major   int
}

func New() *Manager {
	return &Manager{
		events: make(chan Event, 256),
	}
}

func (m *Manager) Events() <-chan Event {
	return m.events
}

func (m *Manager) Start(path, content string, opts RunOptions) int {
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

	go m.run(ctx, id, path, content, opts)
	return id
}

func (m *Manager) StartCommand(command string, args []string, dir string, opts RunOptions) int {
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

	go m.runCommand(ctx, id, command, args, dir, opts)
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

func (m *Manager) StopRun(id int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id != m.seq {
		return
	}
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

func (m *Manager) run(ctx context.Context, id int, path, content string, opts RunOptions) {
	m.events <- StartedMsg{ID: id}

	if opts.MaxRuntime <= 0 {
		opts.MaxRuntime = DefaultMaxRuntime
	}
	if opts.MaxRuntime > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.MaxRuntime)
		defer cancel()
	}

	dir, err := os.MkdirTemp("", "terminal-ide-run-*")
	if err != nil {
		m.events <- FinishedMsg{ID: id, Err: err}
		return
	}
	defer os.RemoveAll(dir)

	prepared, err := prepareRun(ctx, path, content, dir, opts)
	if err != nil {
		m.events <- FinishedMsg{ID: id, Err: err}
		return
	}
	if prepared.cleanup != nil {
		defer prepared.cleanup()
	}
	cmd := prepared.cmd
	m.executeCommand(ctx, id, cmd, opts)
}

func (m *Manager) runCommand(ctx context.Context, id int, command string, args []string, dir string, opts RunOptions) {
	m.events <- StartedMsg{ID: id}
	if opts.MaxRuntime > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, opts.MaxRuntime)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = append([]string{}, os.Environ()...)
	if dir != "" {
		cmd.Dir = dir
	}
	m.executeCommand(ctx, id, cmd, opts)
}

func (m *Manager) executeCommand(ctx context.Context, id int, cmd *exec.Cmd, opts RunOptions) {
	if cmd == nil {
		m.events <- FinishedMsg{ID: id, Err: fmt.Errorf("missing command")}
		return
	}

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
	go m.streamStderr(id, stderr, &wg)
	wg.Wait()
	m.clearStdin(id)

	err = cmd.Wait()
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		timeout := opts.MaxRuntime.Round(time.Second)
		if timeout <= 0 {
			timeout = time.Second
		}
		m.events <- FinishedMsg{
			ID:       id,
			Err:      fmt.Errorf("run timed out after %s", timeout),
			TimedOut: true,
		}
		return
	case errors.Is(ctx.Err(), context.Canceled):
		m.events <- FinishedMsg{ID: id, Cancelled: true}
		return
	}
	m.events <- FinishedMsg{ID: id, Err: err}
}

func (m *Manager) stream(id int, reader io.Reader, isErr bool, wg *sync.WaitGroup) {
	defer wg.Done()

	buf := make([]byte, 256)
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			m.events <- OutputMsg{ID: id, Text: string(buf[:n]), IsErr: isErr}
		}
		if err == io.EOF {
			return
		}
		if err != nil {
			if !strings.Contains(err.Error(), "file already closed") {
				m.events <- OutputMsg{ID: id, Text: err.Error() + "\n", IsErr: true}
			}
			return
		}
	}
}

func (m *Manager) streamStderr(id int, reader io.Reader, wg *sync.WaitGroup) {
	defer wg.Done()

	buf := make([]byte, 256)
	var pending strings.Builder
	for {
		n, err := reader.Read(buf)
		if n > 0 {
			pending.Write(buf[:n])
			m.flushStderrBuffer(id, &pending, err == io.EOF)
		}
		if err == io.EOF {
			return
		}
		if err != nil {
			if !strings.Contains(err.Error(), "file already closed") {
				m.events <- OutputMsg{ID: id, Text: err.Error() + "\n", IsErr: true}
			}
			return
		}
	}
}

func (m *Manager) flushStderrBuffer(id int, pending *strings.Builder, flushRemainder bool) {
	for {
		data := pending.String()
		idx := strings.IndexByte(data, '\n')
		if idx == -1 {
			if flushRemainder && data != "" {
				m.events <- OutputMsg{ID: id, Text: data, IsErr: true}
				pending.Reset()
			}
			return
		}

		line := data[:idx]
		rest := data[idx+1:]
		pending.Reset()
		pending.WriteString(rest)

		if strings.HasPrefix(line, executionEventPrefix) {
			payload := strings.TrimPrefix(line, executionEventPrefix)
			var trace tracedEvent
			if err := json.Unmarshal([]byte(payload), &trace); err == nil {
				m.events <- ExecutionMsg{
					ID:      id,
					File:    trace.File,
					Line:    trace.Line,
					State:   trace.Type,
					Waiting: trace.Type == "waiting_input",
				}
				continue
			}
		}

		m.events <- OutputMsg{ID: id, Text: line + "\n", IsErr: true}
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

type tracedEvent struct {
	Type string `json:"type"`
	File string `json:"file"`
	Line int    `json:"line"`
}

const pythonTraceBootstrap = `
import base64
import json
import os
import sys

SOURCE_TOKEN = sys.argv[1]
ORIGINAL_PATH = sys.argv[2]
CURRENT_LINE = 0
EVENT_PREFIX = "__TUI_EVT__"

try:
    import __builtin__ as _builtins
except ImportError:
    import builtins as _builtins

REAL_INPUT = getattr(_builtins, "raw_input", None)
if REAL_INPUT is None:
    REAL_INPUT = _builtins.input

def emit(event_type, line):
    payload = {
        "type": event_type,
        "file": ORIGINAL_PATH,
        "line": line or 0,
    }
    sys.stderr.write(EVENT_PREFIX + json.dumps(payload) + "\n")
    sys.stderr.flush()

def trace_calls(frame, event, arg):
    global CURRENT_LINE
    if event == "line" and frame.f_code.co_filename == ORIGINAL_PATH:
        CURRENT_LINE = frame.f_lineno
        emit("line", CURRENT_LINE)
    return trace_calls

def traced_input(prompt=""):
    emit("waiting_input", CURRENT_LINE)
    value = REAL_INPUT(prompt)
    emit("resumed", CURRENT_LINE)
    return value

def emit_final(event_type, line=None):
    emit(event_type, line if line is not None else CURRENT_LINE)

if hasattr(_builtins, "raw_input"):
    _builtins.raw_input = traced_input
_builtins.input = traced_input

if SOURCE_TOKEN == "--inline":
    encoded = os.environ.get("TERMINAL_IDE_SOURCE_B64", "")
    source = base64.b64decode(encoded.encode("ascii"))
else:
    with open(SOURCE_TOKEN, "rb") as handle:
        source = handle.read()

sys.argv = [ORIGINAL_PATH]
sys.path[0] = os.path.dirname(ORIGINAL_PATH)
globals_dict = {
    "__name__": "__main__",
    "__file__": ORIGINAL_PATH,
    "__package__": None,
}

sys.settrace(trace_calls)
try:
    code = compile(source, ORIGINAL_PATH, "exec", 0, True)
except BaseException as err:
    emit_final("exception", getattr(err, "lineno", CURRENT_LINE))
    raise

try:
    exec(code, globals_dict)
except SystemExit:
    emit_final("finished")
    raise
except BaseException:
    emit_final("exception")
    raise
else:
    emit_final("finished")
`

func detectPythonInterpreter(ctx context.Context, originalPath, source string) (string, error) {

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
		switch pythonMajorVersion(interpreter) {
		case 2:
			py2Candidates = append(py2Candidates, interpreter)
		case 3:
			py3Candidates = append(py3Candidates, interpreter)
		}
	}

	if detected := pickByCompilationProbe(ctx, originalPath, source, py3Candidates, py2Candidates); detected != "" {
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
	interpreters, _ := DiscoverPythonInterpreters()
	var commands []string
	for _, item := range interpreters {
		commands = append(commands, item.Path)
	}
	return commands
}

func DiscoverPythonInterpreters() ([]PythonInterpreter, error) {
	candidates := []string{
		"python3",
		"python3.12",
		"python3.11",
		"python3.10",
		"python3.9",
		"python3.8",
		"python",
		"python2",
		"python2.7",
	}
	seen := make(map[string]bool)
	var available []PythonInterpreter
	for _, name := range candidates {
		path, err := exec.LookPath(name)
		if err != nil || seen[path] {
			continue
		}
		seen[path] = true
		major, version := pythonVersionDetails(name)
		if major == 0 {
			continue
		}
		available = append(available, PythonInterpreter{
			Command: name,
			Path:    path,
			Version: version,
			Major:   major,
		})
	}
	return available, nil
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
		return resolvePythonAlias(fields[1])
	}

	return resolvePythonAlias(filepath.Base(fields[0]))
}

func resolvePythonAlias(name string) string {
	if _, err := exec.LookPath(name); err == nil {
		return name
	}

	switch {
	case strings.HasPrefix(name, "python2."):
		if _, err := exec.LookPath("python2"); err == nil {
			return "python2"
		}
	case strings.HasPrefix(name, "python3."):
		if _, err := exec.LookPath("python3"); err == nil {
			return "python3"
		}
	}

	return name
}

func pythonMajorVersion(interpreter string) int {
	major, _ := pythonVersionDetails(interpreter)
	return major
}

func pythonVersionDetails(interpreter string) (int, string) {
	cmd := exec.Command(interpreter, "-c", "import sys; print(sys.version_info[0])")
	output, err := cmd.Output()
	if err != nil {
		switch {
		case strings.Contains(interpreter, "python2"):
			return 2, ""
		case strings.Contains(interpreter, "python3"):
			return 3, ""
		default:
			return 0, ""
		}
	}

	major := 0
	switch strings.TrimSpace(string(output)) {
	case "2":
		major = 2
	case "3":
		major = 3
	default:
		return 0, ""
	}

	versionCmd := exec.Command(interpreter, "-c", "import sys; print('%d.%d.%d' % sys.version_info[:3])")
	versionOutput, err := versionCmd.Output()
	if err != nil {
		return major, ""
	}
	return major, strings.TrimSpace(string(versionOutput))
}

func pickByCompilationProbe(ctx context.Context, originalPath, source string, py3Candidates, py2Candidates []string) string {
	py3 := firstCompilingInterpreter(ctx, originalPath, source, py3Candidates)
	py2 := firstCompilingInterpreter(ctx, originalPath, source, py2Candidates)

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

func firstCompilingInterpreter(ctx context.Context, originalPath, source string, interpreters []string) string {
	for _, interpreter := range interpreters {
		if pythonCompiles(ctx, interpreter, originalPath, source) {
			return interpreter
		}
	}
	return ""
}

func pythonCompiles(ctx context.Context, interpreter, originalPath, source string) bool {
	cmd := exec.CommandContext(ctx, interpreter, "-c", "import sys; compile(sys.stdin.read(), sys.argv[1], 'exec')", originalPath)
	cmd.Stdin = strings.NewReader(source)
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
