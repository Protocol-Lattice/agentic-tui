# ✨ Vibe Coder — Agentic Multi-LLM TUI

Vibe Coder is an interactive terminal UI that lets you *vibe code* with a team of
autonomous agents, similar to tools like Claude Code, Gemini Code Assist, or
Codex. It is built on top of the
[Protocol-Lattice go-agent](https://github.com/Protocol-Lattice/go-agent)
stack and Charmbracelet's Bubble Tea TUI framework.

The interface ships with three specialised roles:

| Agent      | Role description                               | Hotkey |
|------------|------------------------------------------------|--------|
| `@architect` | High-level design, planning, and refactoring    | `1`    |
| `@coder`     | Feature and bug-fix implementation              | `2`    |
| `@reviewer`  | Quality, testing, and code review suggestions   | `3`    |

Type your task into the prompt, optionally targeting an agent with
`@architect`, `@coder`, or `@reviewer`. The active agent can also be
selected with the hotkeys.

## ✍️ Key features

* **Multi-agent chat** — switch agents on the fly or start prompts with
  directives such as `@coder: build the CLI parser`.
* **Automatic code application** — generated fenced code blocks are written to
  disk with Git diffs previewed in the log pane.
* **Git integration** — repos can be initialised automatically (`--git`) and
  every update is committed with the agent's response summary.
* **Project context** — prompts include file trees, Git history, and imported
  module documentation fetched through the gitmcp.io proxy.

## 🚀 Getting started

### Prerequisites

* Go 1.21+ installed
* A Google Gemini API key available as `GEMINI_API_KEY`

### Install dependencies

```
go mod download
```

> The first build downloads a fairly large dependency graph, so it may take a
> moment.

### Run the TUI

```
GEMINI_API_KEY=your-key-here go run . --dir /path/to/project --git
```

Use the optional `--ask-dir` flag to pick a working directory at startup.

### Navigating the UI

* `Enter` — send the current prompt
* `1`/`2`/`3` — switch the active agent role
* `Tab` — toggle focus between prompt and output panes
* `PgUp`/`PgDn` — scroll the transcript
* `?` — toggle an in-app quick reference overlay
* `Ctrl+C` or `Esc` — quit the application

## 🤝 Contributing

Pull requests are welcome! Please include screenshots or transcripts for UX
changes and make sure `go fmt`/`go build` succeed locally before submitting.
