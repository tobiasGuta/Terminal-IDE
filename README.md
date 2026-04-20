# Terminal IDE

A terminal IDE written in Go with Bubble Tea, Lip Gloss, and Chroma. It combines a code editor, live runner, workspace browser, and optional AI assistance in a single TUI for Python and Go.

<img width="1415" height="711" alt="image" src="https://github.com/user-attachments/assets/a828455d-fb1a-4adc-b410-0aa377cf91ba" />


## Features

- Welcome screen with `Open File`, `Open Folder`, and `Create New File`
- Multi-tab editor with dirty-state tracking and tab overflow handling
- Persistent workspace sidebar for browsing the current folder tree
- Git status indicators in the sidebar, tabs, and file header
- Split editor and live output layout with capped output scrollback
- Horizontal scrolling in the editor and output panes for long lines
- Debounced live execution for `.py` and `.go`
- Real-time stdout/stderr streaming plus live stdin input
- Python execution tracing with a live execution pointer in the editor
- Per-tab Python interpreter override alongside auto-detection
- Editor quality-of-life features:
  - undo/redo
  - find/replace
  - live find preview with wrapped-search feedback
  - jump to line
  - auto-indent
  - bracket and quote auto-close
  - block selection mode
- Mouse support for editor cursor placement, pane focus, and output selection
- Context-aware footer hints instead of a full static shortcut dump
- Command palette via `Ctrl+P` or `?`
- Optional AI error explanations and hints with streaming responses
- Google Gemini, OpenAI, and Anthropic support
- Config file support for AI keys and default model selection
- Unit tests covering runner, editor, AI client, config, and file picker behavior

## Build

```bash
go mod tidy
go build ./cmd/ide
```

## Run

```bash
go run ./cmd/ide
```

## Test

```bash
go test ./...
```

If you are running in a restricted environment where the default Go cache path is not writable, use:

```bash
GOCACHE=$(pwd)/.gocache go test ./...
```

## Workspace and File Picking

- `Open File` and `Open Folder` start from your current working directory, but you can browse outside it
- After choosing a folder as a workspace, the follow-up file picker is rooted to that folder
- The sidebar shows that workspace as a persistent tree so you can switch files without reopening the picker

## Runner

- Python and Go run through a pluggable language runner registry instead of a hard-coded switch
- Runs have a default 30 second timeout, and timed-out executions are reported in the UI
- Python uses inline execution for small scripts instead of always writing a temp source file
- If `bwrap` is installed, runs use a best-effort Bubblewrap sandbox with network unshared and the filesystem mostly read-only
- If `bwrap` is unavailable, execution falls back to the normal local process model

## AI Setup

AI is optional. If no API key is set, the editor still works normally and AI features stay disabled.

### Config File

You can keep AI credentials and a default model in config files instead of exporting env vars every shell session.

Supported config locations, loaded in this order:

- `~/.config/terminal-ide/config.toml`
- `.terminal-ide.toml` in the current project directory

Environment variables still win over file values.

Example:

```toml
gemini_api_key = "your-gemini-api-key"
openai_api_key = "your-openai-api-key"
anthropic_api_key = "your-anthropic-api-key"
preferred_ai_model = "claude-3-5-sonnet-latest"
```

Environment variables override config-file values:

- `GEMINI_API_KEY`
- `OPENAI_API_KEY`
- `ANTHROPIC_API_KEY`
- `TERMINAL_IDE_AI_RATE_LIMIT`
- `TERMINAL_IDE_AI_PROMPTS_FILE`
- `TERMINAL_IDE_EXPLAIN_SYSTEM_PROMPT`
- `TERMINAL_IDE_HINT_SYSTEM_PROMPT`

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

### Anthropic

If `ANTHROPIC_API_KEY` is configured, Claude models are also available in the model picker.

Available Anthropic models:

- `claude-3-5-sonnet-latest`
- `claude-3-5-haiku-latest`

### Provider Selection Rules

- If `GEMINI_API_KEY` is set, Gemini is preferred by default
- If Gemini is not set but `OPENAI_API_KEY` is set, OpenAI is used
- If multiple providers are set, you can switch models during the session with `Alt+M`
- If neither key is set, AI features stay disabled with no side effects

## How To Use

### Opening and Editing Files

- Start the app with `go run ./cmd/ide`
- Use the welcome screen to open a file, open a folder, or create a new file
- Type directly in the editor pane
- Use the sidebar to switch files inside the active workspace
- The app automatically reruns supported files after a short debounce when you edit
- Use `Alt+Left` / `Alt+Right` in the editor to scroll long lines horizontally

### Running Code

- Python and Go files are run automatically after edits
- Saved files can also be rerun with `Ctrl+S`
- Stdout and stderr appear in the lower output pane
- If your program waits for input, use `Ctrl+L` to focus the live input area, type your response, and press `Enter`
- Long-running programs stop automatically when they hit the max runtime
- When output lines are wider than the pane, focus the output and use `Left` / `Right` to scroll horizontally

### Python Tracing

For Python files, Terminal IDE highlights the currently executing line.

- A running line is shown in the editor while the script executes
- When Python waits on `input()`, the editor status shows the waiting line
- If you change the file while a run is active, the old run is stopped and a fresh run is queued

### AI Error Decoder

Use `Ctrl+E` in the editor screen to ask the AI to explain the current Python error output.

What it does:

- Reads a focused window of the current tab's source code around the relevant line
- Collects the current stderr traceback from the output pane
- Sends both to the active AI provider
- Appends a plain-English explanation under `── AI Explanation ──`

Notes:

- This is manual only; it does not auto-trigger on every error
- If there is no error output to analyze, nothing is sent
- AI requests are rate-limited globally by the AI client
- The request includes the traceback plus a focused code window around the relevant line instead of the full file

### AI Hints

Use `Ctrl+H` in the editor screen to ask for a Socratic hint.

What it does:

- Sends a focused code window around the current execution line
- Includes the current error output, if any
- Includes the current execution line
- Returns a short hint without giving the full answer

Hint behavior:

- Hint level starts at 1
- Pressing `Ctrl+H` again on the same unchanged file increases the hint level up to 3
- Editing the file resets the hint level back to 1
- AI requests are rate-limited globally

### AI Loading Indicator

While an AI request is running:

- the UI stays responsive
- a `[Thinking...]` indicator appears in the footer
- streamed AI chunks are appended live as they arrive

### AI Model Picker

Use `Alt+M` from the editor screen to choose the active AI model for the current session.

Behavior:

- If only one provider is configured, the app auto-selects that provider's first model
- If multiple providers are configured, a picker opens
- The active model name is shown in muted text on the right side of the footer
- If no AI key is configured, the status bar tells you to set AI keys in env vars or config

### Command Palette

Use `Ctrl+P` or `?` to open the command palette.

Behavior:

- Search uses case-insensitive fuzzy subsequence matching
- `Enter` runs the selected command
- `Esc` or `Ctrl+P` closes the palette
- The palette includes navigation, editing, AI, theme, interpreter, and file-management commands

### Footer Hints

The footer is contextual and shows only a few shortcuts relevant to the current screen or focus state.

## Continuous Testing

GitHub Actions runs `go test ./...` on pushes and pull requests.

## Keyboard Shortcuts

- `Ctrl+S` save and rerun
- `Ctrl+Z` undo
- `Ctrl+Y` redo
- `Ctrl+F` find and replace
- `Alt+Left` / `Alt+Right` scroll long editor lines horizontally
- `Ctrl+G` go to line
- `Ctrl+P` command palette
- `?` open command palette
- `Ctrl+O` open file picker
- `Ctrl+W` close current tab
- `Shift+Tab` previous tab
- `Tab` or `Ctrl+]` next tab
- `Ctrl+C` copy selected live output text, or else the current editor line/input buffer
- `Ctrl+V` paste clipboard into the editor or live input
- `Left` / `Right` scroll long output lines horizontally when the output pane is focused
- `Ctrl+L` focus live input
- `Ctrl+R` open the Python interpreter selector for the current Python tab
- `Ctrl+E` request AI error explanation from the current error output
- `Ctrl+H` request an AI code hint
- `Alt+M` open the AI model picker
- `Alt+T` open the theme picker
- `Alt+B` toggle block selection
- `n` create a new file from the welcome screen
- `Esc` return to welcome menu
- `Ctrl+Q` quit

## AI Error Handling

The AI layer handles common API failures with friendly messages.

- Gemini `429`: rate limit reached
- OpenAI `429`: rate limit reached
- Anthropic `429`: rate limit reached
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
- The output pane keeps a bounded scrollback instead of growing without limit
- The general file picker can browse outside the launch directory
- The workspace file picker stays rooted to the folder you explicitly opened
- `Ctrl+M` is not used for the AI model picker because many terminals treat it as `Enter`

## Screenshots
#### Menu

<img width="1912" height="994" alt="image" src="https://github.com/user-attachments/assets/dbcec4a2-2e83-4472-b154-213b760bb5eb" />

----

#### Selecting files

<img width="1912" height="994" alt="image" src="https://github.com/user-attachments/assets/1f3854de-294c-45e8-9f76-c1f1e146d87b" />

----

#### Editor

<img width="1912" height="994" alt="image" src="https://github.com/user-attachments/assets/de72d946-d188-4c3c-b48b-d3e2a900706e" />

----

#### More Files and commands

https://github.com/user-attachments/assets/16f5eb63-738d-42bf-8021-7e635ddb9606

----

#### Find Command

https://github.com/user-attachments/assets/a6d9f257-9b32-4f34-ad3a-bdbda9da9fb8

----

#### trace and errors

https://github.com/user-attachments/assets/5222c9cc-aa4c-468a-b3a6-d0913bf26efe

----

#### AI

https://github.com/user-attachments/assets/a383c30c-a40c-4cef-86dc-43a8bcde25b1

