package ui

import (
	"bytes"
	"fmt"
	"os/exec"
	"runtime"
)

func readClipboard() (string, error) {
	commands := [][]string{
		{"pbpaste"},
		{"wl-paste", "-n"},
		{"xclip", "-selection", "clipboard", "-o"},
		{"xsel", "--clipboard", "--output"},
	}
	if runtime.GOOS == "windows" {
		commands = append([][]string{{"powershell", "-NoProfile", "-Command", "Get-Clipboard"}}, commands...)
	}

	for _, args := range commands {
		path, err := exec.LookPath(args[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(path, args[1:]...)
		out, err := cmd.Output()
		if err == nil {
			return string(out), nil
		}
	}
	return "", fmt.Errorf("clipboard paste is unavailable on this system")
}

func writeClipboard(text string) error {
	commands := [][]string{
		{"pbcopy"},
		{"wl-copy"},
		{"xclip", "-selection", "clipboard"},
		{"xsel", "--clipboard", "--input"},
	}
	if runtime.GOOS == "windows" {
		commands = append([][]string{{"clip"}}, commands...)
	}

	for _, args := range commands {
		path, err := exec.LookPath(args[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(path, args[1:]...)
		cmd.Stdin = bytes.NewBufferString(text)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("clipboard copy is unavailable on this system")
}
