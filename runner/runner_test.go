package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type testLanguageRunner struct{}

func (testLanguageRunner) PrepareRun(req RunRequest) (preparedRun, error) {
	cmd := exec.CommandContext(req.Context, os.Args[0], "-test.run=TestRunnerHelperProcess", "--", req.OriginalPath)
	cmd.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1")
	return preparedRun{cmd: cmd}, nil
}

func TestRunnerHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}

	args := os.Args
	sep := 0
	for i, arg := range args {
		if arg == "--" {
			sep = i
			break
		}
	}
	if sep == 0 || sep+1 >= len(args) {
		fmt.Fprintln(os.Stderr, "missing helper mode")
		os.Exit(2)
	}

	switch args[sep+1] {
	case "echo.test":
		fmt.Println("ready")
		var input string
		if _, err := fmt.Fscanln(os.Stdin, &input); err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Exit(1)
		}
		fmt.Printf("echo:%s\n", input)
	case "sleep.test":
		fmt.Println("started")
		time.Sleep(5 * time.Second)
	default:
		fmt.Fprintln(os.Stderr, "unknown helper mode")
		os.Exit(2)
	}
	os.Exit(0)
}

func TestManagerStartSendInputAndStop(t *testing.T) {
	RegisterLanguageRunner(".test", testLanguageRunner{})

	manager := New()
	id := manager.Start("echo.test", "", RunOptions{DisableSandbox: true, MaxRuntime: 3 * time.Second})
	if id == 0 {
		t.Fatalf("expected non-zero run id")
	}

	expectEventType[StartedMsg](t, manager.Events(), 2*time.Second)
	output := expectEventType[OutputMsg](t, manager.Events(), 2*time.Second)
	if !strings.Contains(output.Text, "ready") {
		t.Fatalf("expected ready output, got %q", output.Text)
	}

	if err := manager.SendInput(id, "hello\n"); err != nil {
		t.Fatalf("SendInput() error = %v", err)
	}

	output = expectEventType[OutputMsg](t, manager.Events(), 2*time.Second)
	if !strings.Contains(output.Text, "echo:hello") {
		t.Fatalf("expected echoed input, got %q", output.Text)
	}

	finished := expectEventType[FinishedMsg](t, manager.Events(), 2*time.Second)
	if finished.Err != nil {
		t.Fatalf("expected nil error, got %v", finished.Err)
	}
	if finished.Cancelled {
		t.Fatalf("expected run not to be cancelled")
	}
}

func TestManagerStopCancelsActiveRun(t *testing.T) {
	RegisterLanguageRunner(".test", testLanguageRunner{})

	manager := New()
	id := manager.Start("sleep.test", "", RunOptions{DisableSandbox: true, MaxRuntime: 10 * time.Second})
	if id == 0 {
		t.Fatalf("expected non-zero run id")
	}

	expectEventType[StartedMsg](t, manager.Events(), 2*time.Second)
	expectEventType[OutputMsg](t, manager.Events(), 2*time.Second)

	manager.StopRun(id)

	finished := expectEventType[FinishedMsg](t, manager.Events(), 2*time.Second)
	if !finished.Cancelled {
		t.Fatalf("expected cancelled run, got %+v", finished)
	}
}

func TestManagerTimeoutMarksFinishedMsg(t *testing.T) {
	RegisterLanguageRunner(".test", testLanguageRunner{})

	manager := New()
	manager.Start("sleep.test", "", RunOptions{DisableSandbox: true, MaxRuntime: 200 * time.Millisecond})

	expectEventType[StartedMsg](t, manager.Events(), 2*time.Second)
	expectEventType[OutputMsg](t, manager.Events(), 2*time.Second)

	finished := expectEventType[FinishedMsg](t, manager.Events(), 2*time.Second)
	if !finished.TimedOut {
		t.Fatalf("expected timed out run, got %+v", finished)
	}
	if finished.Err == nil || !strings.Contains(finished.Err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got %v", finished.Err)
	}
}

func TestManagerStartCommandStreamsOutput(t *testing.T) {
	manager := New()
	id := manager.StartCommand(
		"printf",
		[]string{"ready"},
		"",
		RunOptions{DisableSandbox: true, MaxRuntime: 3 * time.Second},
	)
	if id == 0 {
		t.Fatalf("expected non-zero run id")
	}

	expectEventType[StartedMsg](t, manager.Events(), 2*time.Second)
	output := expectEventType[OutputMsg](t, manager.Events(), 2*time.Second)
	if !strings.Contains(output.Text, "ready") {
		t.Fatalf("expected ready output, got %q", output.Text)
	}
	finished := expectEventType[FinishedMsg](t, manager.Events(), 2*time.Second)
	if finished.Err != nil {
		t.Fatalf("expected nil error, got %v", finished.Err)
	}
}

func expectEventType[T any](t *testing.T, events <-chan Event, timeout time.Duration) T {
	t.Helper()

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	for {
		select {
		case event := <-events:
			if typed, ok := event.(T); ok {
				return typed
			}
		case <-deadline.C:
			var zero T
			t.Fatalf("timed out waiting for %T", zero)
			return zero
		}
	}
}

func TestDetectPythonInterpreterUsesSourceString(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	interpreter, err := detectPythonInterpreter(ctx, "example.py", "print('hi')\n")
	if err != nil && !strings.Contains(err.Error(), "no Python interpreter found") {
		t.Fatalf("unexpected error: %v", err)
	}
	if err == nil && interpreter == "" {
		t.Fatalf("expected interpreter when no error is returned")
	}
}
