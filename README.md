# Terminal IDE

A terminal-based IDE written in Go using Bubble Tea, Lip Gloss, and Chroma.

## Features

- Welcome screen with keyboard-driven `Open File` and `Open Folder` actions
- Welcome screen action to create a new file by entering a name and extension
- Multiple open files with tabs
- Split editor and live output layout
- Custom code editor with:
  - line numbers
  - basic cursor movement
  - typing, delete, backspace, enter, and paste support
- Debounced live execution for `.py` and `.go` files
- Real-time stdout/stderr streaming into the lower pane
- Live stdin input for programs that prompt with `input()` or `raw_input()`
- Clipboard copy/paste for the current editor line or live input
- Mouse click support for focusing the editor or output pane
- Keyboard shortcuts:
  - `Ctrl+S` save
  - `Ctrl+O` open file picker
  - `Ctrl+W` close current tab
  - `Shift+Tab` previous tab
  - `Tab` or `Ctrl+]` next tab
  - `Ctrl+C` copy current editor line or input buffer
  - `Ctrl+V` paste clipboard into the editor or live input
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

## Screenshots

<img width="1922" height="960" alt="Screenshot From 2026-04-17 11-49-44" src="https://github.com/user-attachments/assets/7c133d64-ecd7-473b-be9b-3fbded5d3bd5" />


----

<img width="1922" height="960" alt="Screenshot From 2026-04-17 11-49-50" src="https://github.com/user-attachments/assets/0e7505ac-18c4-4c29-ac65-a017e06067f8" />

----

<img width="1922" height="960" alt="Screenshot From 2026-04-17 11-50-00" src="https://github.com/user-attachments/assets/4a68b964-f9f3-4bbb-b860-2632b9bd3a4d" />

----

<img width="1922" height="960" alt="Screenshot From 2026-04-17 11-50-08" src="https://github.com/user-attachments/assets/1825cca6-36f5-4e7e-a409-18bc6d3ac4b4" />
