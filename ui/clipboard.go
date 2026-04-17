package ui

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/aymanbagabas/go-osc52/v2"
)

type clipboardBackend struct {
	copy  []string
	paste []string
}

func readClipboard() (string, error) {
	for _, backend := range clipboardBackends() {
		path, err := exec.LookPath(backend.paste[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(path, backend.paste[1:]...)
		out, err := cmd.Output()
		if err == nil {
			return string(out), nil
		}
	}
	return "", fmt.Errorf("clipboard paste is unavailable on this system")
}

func writeClipboard(text string) error {
	for _, backend := range clipboardBackends() {
		path, err := exec.LookPath(backend.copy[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(path, backend.copy[1:]...)
		cmd.Stdin = bytes.NewBufferString(text)
		if err := cmd.Run(); err != nil {
			continue
		}
		if verifyClipboardBackend(backend, text) {
			return nil
		}
	}

	if strings.TrimSpace(text) != "" && writeClipboardOSC52(text) == nil {
		return nil
	}
	return fmt.Errorf("clipboard copy is unavailable on this system")
}

func clipboardBackends() []clipboardBackend {
	switch runtime.GOOS {
	case "darwin":
		return []clipboardBackend{
			{copy: []string{"pbcopy"}, paste: []string{"pbpaste"}},
		}
	case "windows":
		return []clipboardBackend{
			{copy: []string{"clip"}, paste: []string{"powershell", "-NoProfile", "-Command", "Get-Clipboard"}},
		}
	default:
		var backends []clipboardBackend
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			backends = append(backends, clipboardBackend{
				copy:  []string{"wl-copy", "--type", "text/plain;charset=utf-8"},
				paste: []string{"wl-paste", "-n"},
			})
		}
		if os.Getenv("DISPLAY") != "" {
			backends = append(backends,
				clipboardBackend{
					copy:  []string{"xclip", "-in", "-selection", "clipboard"},
					paste: []string{"xclip", "-selection", "clipboard", "-o"},
				},
				clipboardBackend{
					copy:  []string{"xsel", "--clipboard", "--input"},
					paste: []string{"xsel", "--clipboard", "--output"},
				},
			)
		}
		return backends
	}
}

func verifyClipboardBackend(backend clipboardBackend, want string) bool {
	if len(backend.paste) == 0 {
		return true
	}
	path, err := exec.LookPath(backend.paste[0])
	if err != nil {
		return false
	}
	cmd := exec.Command(path, backend.paste[1:]...)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return normalizeClipboardText(string(out)) == normalizeClipboardText(want)
}

func normalizeClipboardText(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	return strings.TrimSuffix(text, "\n")
}

func writeClipboardOSC52(text string) error {
	tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer tty.Close()

	seq := osc52.New(text)
	if os.Getenv("TMUX") != "" {
		seq = seq.Tmux()
	} else if strings.HasPrefix(os.Getenv("TERM"), "screen") {
		seq = seq.Screen()
	}
	_, err = seq.WriteTo(tty)
	return err
}
