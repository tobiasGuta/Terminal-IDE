# Terminal IDE

A terminal-based IDE written in Go using Bubble Tea, Lip Gloss, and Chroma. It includes a split editor/output layout, live execution for Python and Go, Python execution tracing, live stdin support, and optional AI-powered error explanations and hints.

## Features

- Welcome screen with keyboard-driven `Open File`, `Open Folder`, and `Create New File`
- Multiple open files with tabs
- Split editor and live output layout
- Debounced live execution for `.py` and `.go`
- Real-time stdout/stderr streaming into the output pane
- Live stdin input for programs that prompt with `input()` or `raw_input()`
- Python execution tracing with a live execution pointer in the editor
- Manual Python interpreter override per tab, alongside auto-detection
- Clipboard copy/paste for the editor, live input, and selected live output text
- Mouse click support for focusing the editor or output pane
- Mouse drag selection in the live output pane
- AI Error Decoder via `Ctrl+E`
- Socratic AI hints via `Ctrl+H`
- AI model selection via `Alt+M`
- Google Gemini and OpenAI support with automatic provider selection

## Build

```bash
go mod tidy
go build ./cmd/ide
```

## Run

```bash
go run ./cmd/ide
```

## AI Setup

AI is optional. If no API key is set, the editor still works normally and AI features stay disabled.

### Recommended: Google Gemini

Gemini is the default AI provider when `GEMINI_API_KEY` is set.

```bash
export GEMINI_API_KEY="your-gemini-api-key" go run ./cmd/ide
```

Default Gemini model:

- `gemini-2.5-flash`

Other Gemini models available in the picker:

- `gemini-2.0-flash`
- `gemini-1.5-flash`
- `gemini-1.5-pro`

### OpenAI

If Gemini is not configured but `OPENAI_API_KEY` is set, the app falls back to OpenAI.

```bash
export OPENAI_API_KEY="your-openai-api-key" go run ./cmd/ide
```

Available OpenAI models:

- `gpt-4o-mini`
- `gpt-4o`

### Provider Selection Rules

- If `GEMINI_API_KEY` is set, Gemini is preferred by default
- If Gemini is not set but `OPENAI_API_KEY` is set, OpenAI is used
- If both are set, you can switch models during the session with `Alt+M`
- If neither key is set, AI features stay disabled with no side effects

## How To Use

### Opening and Editing Files

- Start the app with `go run ./cmd/ide`
- Use the welcome screen to open a file, open a folder, or create a new file
- Type directly in the editor pane
- The app automatically reruns supported files after a short debounce when you edit

### Running Code

- Python and Go files are run automatically after edits
- Saved files can also be rerun with `Ctrl+S`
- Stdout and stderr appear in the lower output pane
- If your program waits for input, use `Ctrl+L` to focus the live input area, type your response, and press `Enter`

### Python Tracing

For Python files, Terminal IDE highlights the currently executing line.

- A running line is shown in the editor while the script executes
- When Python waits on `input()`, the editor status shows the waiting line
- If you change the file while a run is active, the old run is stopped and a fresh run is queued

### AI Error Decoder

Use `Ctrl+E` in the editor screen to ask the AI to explain the current Python error output.

What it does:

- Reads the current tab's source code
- Collects the current stderr traceback from the output pane
- Sends both to the active AI provider
- Appends a plain-English explanation under `── AI Explanation ──`

Notes:

- This is manual only; it does not auto-trigger on every error
- If there is no error output to analyze, nothing is sent
- AI requests are rate-limited to one request every 10 seconds per tab

### AI Hints

Use `Ctrl+H` in the editor screen to ask for a Socratic hint.

What it does:

- Sends the current file contents
- Includes the current error output, if any
- Includes the current execution line
- Returns a short hint without giving the full answer

Hint behavior:

- Hint level starts at 1
- Pressing `Ctrl+H` again on the same unchanged file increases the hint level up to 3
- Editing the file resets the hint level back to 1
- AI requests are rate-limited to one request every 10 seconds per tab

### AI Loading Indicator

While an AI request is running:

- the UI stays responsive
- a `[Thinking...]` indicator appears in the footer
- the result is appended when the request finishes

### AI Model Picker

Use `Alt+M` from the editor screen to choose the active AI model for the current session.

Behavior:

- If only one provider is configured, the app auto-selects that provider's first model
- If both Gemini and OpenAI are configured, a picker opens
- The active model name is shown in muted text on the right side of the footer
- If no AI key is configured, the status bar tells you to set `GEMINI_API_KEY` or `OPENAI_API_KEY`

## Keyboard Shortcuts

- `Ctrl+S` save and rerun
- `Ctrl+O` open file picker
- `Ctrl+W` close current tab
- `Shift+Tab` previous tab
- `Tab` or `Ctrl+]` next tab
- `Ctrl+C` copy selected live output text, or else the current editor line/input buffer
- `Ctrl+V` paste clipboard into the editor or live input
- `Ctrl+L` focus live input
- `Ctrl+R` open the Python interpreter selector for the current Python tab
- `Ctrl+E` request AI error explanation from the current error output
- `Ctrl+H` request an AI code hint
- `Alt+M` open the AI model picker
- `Esc` return to welcome menu
- `Ctrl+Q` quit

## AI Error Handling

The AI layer handles common API failures with friendly messages.

- Gemini `429`: rate limit reached
- OpenAI `429`: rate limit reached
- `4xx`: likely API key or request issue
- `5xx`: temporary service issue
- timeout: connection or service delay
- parse failure: invalid or unexpected AI response

Gemini requests also use exponential backoff retry logic for `429` and `503`.

## Notes

- Python syntax highlighting is included through Chroma
- Live execution currently supports `.py` and `.go`
- Python files try to pick the right interpreter automatically:
  - explicit shebangs like `#!/usr/bin/env python2` are respected
  - otherwise the app probes available Python interpreters and falls back to syntax hints
- Python tabs can override `auto` mode by opening the interpreter selector with `Ctrl+R`
- Python files are launched in unbuffered mode so prompts from `input()` and `raw_input()` show immediately
- Python runs emit live execution-line events so the editor can highlight the current line and pause on blocking input
- `Open Folder` lets you choose a directory first, then browse files inside it
- `Ctrl+M` is not used for the AI model picker because many terminals treat it as `Enter`

## Screenshots

<img width="1922" height="960" alt="Screenshot From 2026-04-17 11-49-44" src="https://github.com/user-attachments/assets/7c133d64-ecd7-473b-be9b-3fbded5d3bd5" />

----

<img width="1922" height="960" alt="Screenshot From 2026-04-17 11-49-50" src="https://github.com/user-attachments/assets/0e7505ac-18c4-4c29-ac65-a017e06067f8" />

----

<img width="1922" height="960" alt="Screenshot From 2026-04-17 11-50-00" src="https://github.com/user-attachments/assets/4a68b964-f9f3-4bbb-b860-2632b9bd3a4d" />

----

<img width="1922" height="960" alt="Screenshot From 2026-04-17 11-50-08" src="https://github.com/user-attachments/assets/1825cca6-36f5-4e7e-a409-18bc6d3ac4b4" />

----

https://github.com/user-attachments/assets/09de9aaa-8242-43d1-b2f9-c98f8a584418

----

### AI

https://github.com/user-attachments/assets/65b0ebc9-f699-4bb4-9631-ec69534666ab

----
