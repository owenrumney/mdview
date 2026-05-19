# mdview

A small CLI that renders a Markdown file to HTML and opens it in your browser. GitHub-flavored Markdown, syntax highlighting, Mermaid diagrams, and an optional live-reload watch mode.

## Install

### MacOS - Homebrew

The quickest way on MacOS is to install with brew

```
brew tap owenrumney/tools
brew install --cask mdview
```

```bash
go install github.com/owenrumney/mdview/cmd/mdview@latest
```

Or build from source:

```bash
make install 
```

## Usage

```bash
mdview path/to/file.md
mdview path/to/markdown-directory
```

One-shot mode renders a file to a temp HTML file and opens it in your default browser. Directory mode starts the local watch server, enables the contents sidebar, and shows a collapsible tree of markdown files found under that directory. Files are rendered lazily when opened in the tree.

If the input file does not exist, mdview asks whether to create it before rendering. Answer `y`/`yes` to create an empty file and start work, or press Enter to abort.

### Flags

| Flag | Description |
| --- | --- |
| `-w`, `--watch` | Serve the file over a local HTTP server and reload the browser on changes |
| `--pdf` | Render to PDF (next to the input file) and open it. Requires Chrome/Chromium/Edge/Brave |
| `--html` | Render to HTML (next to the input file) and open it |
| `--force` | Overwrite an existing output file when using `--pdf` or `--html` |
| `-c`, `--contents` | Show a table-of-contents sidebar with links to each heading |
| `--chat` | Show an experimental document chat sidebar; implies `--watch` and defaults to the `pi` backend |
| `--chat-agent` | Experimental chat backend name for `--chat` |
| `--chat-session` | Experimental chat backend session identifier for `--chat` |
| `-l`, `--light` | Light theme (default is dark) |
| `--unsafe` | Allow raw HTML in markdown and relaxed Mermaid security. Only use on trusted files |
| `-v`, `--version` | Print version info |
| `-h`, `--help` | Show help |

`--watch`, `--pdf`, and `--html` are mutually exclusive. If the output file already exists, use `--force` to overwrite it.

Set `MDVIEW_BROWSER` to force a browser instead of the OS default, e.g. `MDVIEW_BROWSER=Firefox mdview -w README.md` on macOS.

### Examples

Render once and open:

```bash
mdview README.md
```

Live preview while editing:

```bash
mdview --watch docs/guide.md
```

Open a directory of markdown files with a collapsible tree explorer:

```bash
mdview docs
```

Experimental chat sidebar with Pi:

```bash
mdview --chat docs/guide.md
```

`--chat` implies watch mode and defaults to the `pi` backend. mdview starts a background `pi --mode rpc` process, sends chat messages to it over JSONL, and reuses that process until mdview exits. The chat box is focused automatically on open and after replies.

If `pi` is not on your `PATH`, point mdview at it:

```bash
MDVIEW_PI_BIN=/path/to/pi mdview --chat docs/guide.md
```

Watchtower can launch mdview with `--chat-agent watchtower --chat-session <id>` to bridge prompts to a managed session instead of spawning Pi directly.

Light mode:

```bash
mdview --light notes.md
```

Render to PDF and open in the system PDF viewer (e.g. macOS Preview). Headings become bookmarks in the navigation sidebar:

```bash
mdview --pdf notes.md   # writes notes.pdf alongside the input
mdview --pdf --force notes.md  # overwrites notes.pdf if it already exists
```

Render to HTML and open in the browser:

```bash
mdview --html notes.md  # writes notes.html alongside the input
mdview --html --force notes.md # overwrites notes.html if it already exists
```

## Pi bridge

The Pi bridge is built in; there is no separate server to run.

When you run `mdview --chat file.md`, mdview:

1. starts the watch server and opens the document in your browser;
2. creates a private token for the browser chat endpoint;
3. starts `pi --mode rpc --session-dir <temp-dir> --append-system-prompt <mdview prompt>`;
4. forwards each browser chat message to Pi as a JSONL `prompt` command;
5. returns Pi's assistant text to the browser and renders it as Markdown.

The bridge code lives in `internal/server/pi_bridge.go`. It keeps one Pi RPC process alive for the mdview session so follow-up questions share context and responses are faster than spawning Pi for each message.

Useful environment variables:

| Variable | Description |
| --- | --- |
| `MDVIEW_PI_BIN` | Path to the `pi` executable if it is not on `PATH` |
| `MDVIEW_BROWSER` | Browser opener override, e.g. `Firefox` on macOS |
| `MDVIEW_WATCHTOWER_BIN` | Path to `watchtower` when using `--chat-agent watchtower` |

If Pi is not installed or authenticated, `mdview --chat` will fail with the Pi startup error. Configure Pi normally first (for example, run `pi` and complete login/model setup), then retry `mdview --chat file.md`.

## Features

- GitHub-flavored Markdown (tables, task lists, strikethrough, autolinks)
- Footnotes and definition lists
- Syntax highlighting via Chroma (`github-dark` / `github`)
- Mermaid diagrams (bundled, no network required)
- Copy-to-clipboard buttons on code blocks
- Click images or Mermaid diagrams to zoom (Esc or click backdrop to close)
- Live reload over Server-Sent Events when `--watch` is set
- Experimental document chat sidebar in watch mode, backed by Pi RPC by default
- PDF export with heading bookmarks when `--pdf` is set
- HTML export when `--html` is set
- Optional table-of-contents sidebar with `--contents`
- Directory mode with a recursive markdown tree explorer and lazy file rendering
- Auto-generated heading IDs

## Development

```bash
make build           # build to ./bin/mdview
make test            # go test ./...
make fmt             # gofmt -s -w .
make vet             # go vet ./...
make lint            # golangci-lint run
make tidy            # go mod tidy
make update-mermaid  # refresh bundled mermaid.min.js
make release-snapshot
make clean
```

The bundled Mermaid version is controlled by `MERMAID_VERSION` in the Makefile.

## Project layout

```
cmd/mdview/         # CLI entrypoint
internal/render/    # Markdown -> HTML, embedded assets, Mermaid extension
internal/server/    # Watch-mode HTTP server, SSE live reload, chat backends, Pi bridge
internal/pdf/       # Headless-Chrome PDF generation
internal/browser/   # Cross-platform "open URL" helper
examples/           # Sample markdown
```

## License

MIT — see [LICENSE](LICENSE).

