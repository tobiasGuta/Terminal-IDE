package ui

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"strings"
)

func loadGitStatus(root string) map[string]string {
	root = strings.TrimSpace(root)
	if root == "" {
		return map[string]string{}
	}

	cmd := exec.Command("git", "-C", root, "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return map[string]string{}
	}

	statuses := make(map[string]string)
	for _, line := range strings.Split(string(bytes.TrimSpace(output)), "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) < 4 {
			continue
		}
		code := strings.TrimSpace(line[:2])
		pathText := strings.TrimSpace(line[3:])
		if pathText == "" {
			continue
		}
		if idx := strings.Index(pathText, " -> "); idx >= 0 {
			pathText = pathText[idx+4:]
		}
		abs := filepath.Clean(filepath.Join(root, pathText))
		statuses[abs] = shortGitStatus(code)
	}
	return statuses
}

func shortGitStatus(code string) string {
	switch {
	case strings.Contains(code, "??"):
		return "?"
	case strings.Contains(code, "M"):
		return "M"
	case strings.Contains(code, "A"):
		return "A"
	case strings.Contains(code, "D"):
		return "D"
	case strings.Contains(code, "R"):
		return "R"
	default:
		return ""
	}
}
