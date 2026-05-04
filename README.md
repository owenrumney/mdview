# mdview

A small CLI that renders a Markdown file to HTML and opens it in your browser. GitHub-flavored Markdown, syntax highlighting, Mermaid diagrams, and an optional live-reload watch mode.

## Install

```bash
go install github.com/owenrumney/mdview/cmd/mdview@latest
```

Or build from source:

```bash
make build   # produces ./bin/mdview
make install # go install ./cmd/mdview
```

## Usage

```bash
mdview path/to/file.md
```

One-shot mode renders the file to a temp HTML file and opens it in your default browser.

### Flags

| Flag | Description |
| --- | --- |
| `-w`, `--watch` | Serve the file over a local HTTP server and reload the browser on changes |
| `--pdf` | Render to PDF (next to the input file) and open it. Requires Chrome/Chromium/Edge/Brave |
| `--light` | Light theme (default is dark) |
| `--unsafe` | Allow raw HTML in markdown and relaxed Mermaid security. Only use on trusted files |
| `--version` | Print version info |

`--watch` and `--pdf` are mutually exclusive.

### Examples

Render once and open:

```bash
mdview README.md
```

Live preview while editing:

```bash
mdview --watch docs/guide.md
```

Light mode:

```bash
mdview --light notes.md
```

Render to PDF and open in the system PDF viewer (e.g. macOS Preview). Headings become bookmarks in the navigation sidebar:

```bash
mdview --pdf notes.md   # writes notes.pdf alongside the input
```

## Features

- GitHub-flavored Markdown (tables, task lists, strikethrough, autolinks)
- Footnotes and definition lists
- Syntax highlighting via Chroma (`github-dark` / `github`)
- Mermaid diagrams (bundled, no network required)
- Copy-to-clipboard buttons on code blocks
- Click images or Mermaid diagrams to zoom (Esc or click backdrop to close)
- Live reload over Server-Sent Events when `--watch` is set
- PDF export with heading bookmarks when `--pdf` is set
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
internal/server/    # Watch-mode HTTP server with SSE live reload
internal/pdf/       # Headless-Chrome PDF generation
internal/browser/   # Cross-platform "open URL" helper
examples/           # Sample markdown
```

## License

MIT — see [LICENSE](LICENSE).
