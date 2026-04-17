# Terminal IDE

A terminal-based IDE written in Go using Bubble Tea, Lip Gloss, and Chroma.

## Features

- Welcome screen with keyboard-driven `Open File` and `Open Folder` actions
- Multiple open files with tabs
- Split editor and live output layout
- Custom code editor with:
  - line numbers
  - basic cursor movement
  - typing, delete, backspace, enter, and paste support
- Debounced live execution for `.py` and `.go` files
- Real-time stdout/stderr streaming into the lower pane
- Live stdin input for programs that prompt with `input()` or `raw_input()`
- Keyboard shortcuts:
  - `Ctrl+S` save
  - `Ctrl+O` open file picker
  - `Ctrl+W` close current tab
  - `Shift+Tab` previous tab
  - `Tab` or `Ctrl+]` next tab
  - `Ctrl+L` focus live input
  - `Ctrl+E` return focus to the editor
  - `Esc` return to welcome menu
  - `Ctrl+Q` quit

## Build

```bash
go mod tidy
go build ./cmd/ide
```

## Run

```bash
go run ./cmd/ide
```

## Notes

- Python syntax highlighting is included through Chroma.
- Live execution currently supports `.py` and `.go` files.
- Python files try to pick the right interpreter automatically:
  - explicit shebangs like `#!/usr/bin/env python2` are respected
  - otherwise the app probes available Python interpreters and falls back to syntax hints
- `Open Folder` lets you choose a directory first, then browse files inside it.
