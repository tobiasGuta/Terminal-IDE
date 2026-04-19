package runner

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const DefaultMaxRuntime = 30 * time.Second

const maxInlinePythonSourceBytes = 64 * 1024

type RunRequest struct {
	Context      context.Context
	OriginalPath string
	Content      string
	TempDir      string
	Options      RunOptions
}

type preparedRun struct {
	cmd     *exec.Cmd
	cleanup func()
}

type LanguageRunner interface {
	PrepareRun(req RunRequest) (preparedRun, error)
}

var (
	languageRunnersMu sync.RWMutex
	languageRunners   = map[string]LanguageRunner{}
)

func init() {
	RegisterLanguageRunner(".py", pythonLanguageRunner{})
	RegisterLanguageRunner(".go", goLanguageRunner{})
}

func RegisterLanguageRunner(ext string, runner LanguageRunner) {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == "" || runner == nil {
		return
	}
	languageRunnersMu.Lock()
	defer languageRunnersMu.Unlock()
	languageRunners[ext] = runner
}

func prepareRun(ctx context.Context, originalPath, content, tempDir string, opts RunOptions) (preparedRun, error) {
	languageRunnersMu.RLock()
	runner := languageRunners[strings.ToLower(filepath.Ext(originalPath))]
	languageRunnersMu.RUnlock()
	if runner == nil {
		return preparedRun{}, fmt.Errorf("live run supports .py and .go files right now")
	}

	prepared, err := runner.PrepareRun(RunRequest{
		Context:      ctx,
		OriginalPath: originalPath,
		Content:      content,
		TempDir:      tempDir,
		Options:      opts,
	})
	if err != nil {
		return preparedRun{}, err
	}

	if prepared.cmd == nil {
		if prepared.cleanup != nil {
			prepared.cleanup()
		}
		return preparedRun{}, fmt.Errorf("language runner did not return a command")
	}

	if prepared.cmd.Dir == "" {
		prepared.cmd.Dir = filepath.Dir(originalPath)
	}

	if !opts.DisableSandbox {
		sandboxed, err := sandboxCommand(ctx, prepared.cmd, filepath.Dir(originalPath), tempDir)
		if err != nil {
			if prepared.cleanup != nil {
				prepared.cleanup()
			}
			return preparedRun{}, err
		}
		prepared.cmd = sandboxed
	}

	return prepared, nil
}

type pythonLanguageRunner struct{}

func (pythonLanguageRunner) PrepareRun(req RunRequest) (preparedRun, error) {
	interpreter := req.Options.PythonInterpreter
	var err error
	if interpreter == "" {
		interpreter, err = detectPythonInterpreter(req.Context, req.OriginalPath, req.Content)
	} else if _, err = exec.LookPath(interpreter); err != nil {
		if _, statErr := os.Stat(interpreter); statErr != nil {
			return preparedRun{}, fmt.Errorf("selected interpreter %q is not available", interpreter)
		}
	}
	if err != nil {
		return preparedRun{}, err
	}

	env := append([]string{}, os.Environ()...)
	env = append(env, "PYTHONUNBUFFERED=1")

	var cmd *exec.Cmd
	if len(req.Content) <= maxInlinePythonSourceBytes {
		encoded := base64.StdEncoding.EncodeToString([]byte(req.Content))
		env = append(env, "TERMINAL_IDE_SOURCE_B64="+encoded)
		cmd = exec.CommandContext(req.Context, interpreter, "-u", "-c", pythonTraceBootstrap, "--inline", req.OriginalPath)
	} else {
		base := filepath.Base(req.OriginalPath)
		if base == "." || base == string(filepath.Separator) || base == "" {
			base = "snippet.py"
		}
		tempPath := filepath.Join(req.TempDir, base)
		if err := os.WriteFile(tempPath, []byte(req.Content), 0o600); err != nil {
			return preparedRun{}, err
		}
		cmd = exec.CommandContext(req.Context, interpreter, "-u", "-c", pythonTraceBootstrap, tempPath, req.OriginalPath)
	}

	cmd.Env = env
	cmd.Dir = filepath.Dir(req.OriginalPath)
	return preparedRun{cmd: cmd}, nil
}

type goLanguageRunner struct{}

func (goLanguageRunner) PrepareRun(req RunRequest) (preparedRun, error) {
	base := filepath.Base(req.OriginalPath)
	if base == "." || base == string(filepath.Separator) || base == "" {
		base = "snippet.go"
	}
	tempPath := filepath.Join(req.TempDir, base)
	if err := os.WriteFile(tempPath, []byte(req.Content), 0o600); err != nil {
		return preparedRun{}, err
	}

	cacheDir := filepath.Join(req.TempDir, "go-cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return preparedRun{}, err
	}

	cmd := exec.CommandContext(req.Context, "go", "run", tempPath)
	cmd.Dir = filepath.Dir(req.OriginalPath)
	cmd.Env = append([]string{}, os.Environ()...)
	cmd.Env = append(cmd.Env,
		"TMPDIR="+req.TempDir,
		"GOCACHE="+cacheDir,
	)
	return preparedRun{cmd: cmd}, nil
}

func sandboxCommand(ctx context.Context, inner *exec.Cmd, workDir, tempDir string) (*exec.Cmd, error) {
	if _, err := exec.LookPath("bwrap"); err != nil {
		return inner, nil
	}

	workDir, err := filepath.Abs(workDir)
	if err != nil {
		return nil, err
	}
	tempDir, err = filepath.Abs(tempDir)
	if err != nil {
		return nil, err
	}

	chdir := inner.Dir
	if chdir == "" {
		chdir = workDir
	}
	chdir, err = filepath.Abs(chdir)
	if err != nil {
		return nil, err
	}

	args := []string{
		"--die-with-parent",
		"--new-session",
		"--unshare-net",
		"--proc", "/proc",
		"--dev", "/dev",
		"--ro-bind", "/", "/",
	}

	seen := map[string]bool{}
	for _, path := range []string{workDir, tempDir} {
		if path == "" || seen[path] {
			continue
		}
		args = append(args, "--bind", path, path)
		seen[path] = true
	}
	args = append(args, "--chdir", chdir, "--", inner.Path)
	args = append(args, inner.Args[1:]...)

	cmd := exec.CommandContext(ctx, "bwrap", args...)
	cmd.Env = effectiveEnv(inner.Env)
	cmd.Env = upsertEnv(cmd.Env, "HOME", tempDir)
	cmd.Env = upsertEnv(cmd.Env, "TMPDIR", tempDir)
	return cmd, nil
}

func effectiveEnv(env []string) []string {
	if len(env) == 0 {
		return append([]string{}, os.Environ()...)
	}
	return append([]string{}, env...)
}

func upsertEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}
