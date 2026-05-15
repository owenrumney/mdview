package render

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"regexp"

	"github.com/yuin/goldmark"
	highlighting "github.com/yuin/goldmark-highlighting/v2"
	meta "github.com/yuin/goldmark-meta"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	gmhtml "github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
)

type TOCEntry struct {
	Level int
	Text  string
	ID    string
}

type Theme string

const (
	ThemeDark  Theme = "dark"
	ThemeLight Theme = "light"
)

//go:embed assets/mermaid.min.js
var MermaidJS []byte

//go:embed assets/copy-buttons.js
var copyJS string

//go:embed assets/zoom.js
var zoomJS string

//go:embed assets/mermaid-init.js
var mermaidInitJS string

//go:embed assets/style.css
var styleCSS string

//go:embed page.html.tmpl
var pageTmplSrc string

var (
	pageTmpl      = template.Must(template.New("page").Parse(pageTmplSrc))
	scriptCloseRe = regexp.MustCompile(`(?i)</script`)
)

type Options struct {
	WatchMode bool
	Title     string
	Theme     Theme
	Unsafe    bool
	Contents  bool
}

type pageData struct {
	Title           string
	Body            template.HTML
	StyleCSS        template.CSS
	CopyJS          template.JS
	ZoomJS          template.JS
	MermaidInit     template.JS
	WatchMode       bool
	Theme           string
	MermaidTheme    string
	MermaidSecurity string
	MermaidInline   template.JS
	Contents        bool
	TOC             []TOCEntry
}

func File(path string, opts Options) ([]byte, error) {
	src, err := os.ReadFile(path) // #nosec G304 -- path comes from CLI arg by design
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if opts.Theme == "" {
		opts.Theme = ThemeDark
	}
	body, toc, metaTitle, err := toHTML(src, opts.Theme, opts.Unsafe)
	if err != nil {
		return nil, err
	}
	title := opts.Title
	if title == "" {
		title = metaTitle
	}
	if title == "" {
		title = filepath.Base(path)
	}
	data := pageData{
		Title:           title,
		Body:            template.HTML(body),         // #nosec G203 -- output of trusted goldmark renderer
		StyleCSS:        template.CSS(styleCSS),      // #nosec G203 -- embedded constant
		CopyJS:          template.JS(copyJS),         // #nosec G203 -- embedded constant
		ZoomJS:          template.JS(zoomJS),         // #nosec G203 -- embedded constant
		MermaidInit:     template.JS(mermaidInitJS),  // #nosec G203 -- embedded constant
		WatchMode:       opts.WatchMode,
		Theme:           string(opts.Theme),
		MermaidTheme:    mermaidThemeFor(opts.Theme),
		MermaidSecurity: mermaidSecurityFor(opts.Unsafe),
		Contents:        opts.Contents && len(toc) > 0,
		TOC:             toc,
	}
	if !opts.WatchMode {
		safe := scriptCloseRe.ReplaceAllString(string(MermaidJS), `<\/script`)
		data.MermaidInline = template.JS(safe) // #nosec G203 -- embedded constant with </script> escaped
	}
	var buf bytes.Buffer
	if err := pageTmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("render template: %w", err)
	}
	return buf.Bytes(), nil
}

func TempFilePath(sourcePath string) (string, error) {
	abs, err := filepath.Abs(sourcePath)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256([]byte(abs))
	name := fmt.Sprintf("mdview-%s.html", hex.EncodeToString(h[:8]))
	return filepath.Join(os.TempDir(), name), nil
}

func toHTML(src []byte, theme Theme, unsafe bool) ([]byte, []TOCEntry, string, error) {
	chromaStyle := "github-dark"
	if theme == ThemeLight {
		chromaStyle = "github"
	}
	var rendererOpts []renderer.Option
	if unsafe {
		rendererOpts = append(rendererOpts, gmhtml.WithUnsafe())
	}
	md := goldmark.New(
		goldmark.WithExtensions(
			extension.GFM,
			extension.Footnote,
			extension.DefinitionList,
			highlighting.NewHighlighting(
				highlighting.WithStyle(chromaStyle),
			),
			&mermaidExt{},
			meta.Meta,
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(rendererOpts...),
	)
	pc := parser.NewContext()
	doc := md.Parser().Parse(text.NewReader(src), parser.WithContext(pc))
	toc := extractTOC(doc, src)
	var buf bytes.Buffer
	if err := md.Renderer().Render(&buf, src, doc); err != nil {
		return nil, nil, "", fmt.Errorf("convert markdown: %w", err)
	}
	return buf.Bytes(), toc, metaTitle(meta.Get(pc)), nil
}

func metaTitle(m map[string]any) string {
	for _, k := range []string{"title", "name"} {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func extractTOC(doc ast.Node, src []byte) []TOCEntry {
	var toc []TOCEntry
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		h, ok := n.(*ast.Heading)
		if !ok {
			return ast.WalkContinue, nil
		}
		id := ""
		if v, ok := h.AttributeString("id"); ok {
			if b, ok := v.([]byte); ok {
				id = string(b)
			}
		}
		if id == "" {
			return ast.WalkContinue, nil
		}
		toc = append(toc, TOCEntry{
			Level: h.Level,
			Text:  headingText(h, src),
			ID:    id,
		})
		return ast.WalkContinue, nil
	})
	return toc
}

func headingText(h *ast.Heading, src []byte) string {
	var buf bytes.Buffer
	_ = ast.Walk(h, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch v := n.(type) {
		case *ast.Text:
			buf.Write(v.Segment.Value(src))
		case *ast.String:
			buf.Write(v.Value)
		case *ast.AutoLink:
			buf.Write(v.Label(src))
		}
		return ast.WalkContinue, nil
	})
	return buf.String()
}

func mermaidThemeFor(t Theme) string {
	if t == ThemeLight {
		return "default"
	}
	return "dark"
}

func mermaidSecurityFor(unsafe bool) string {
	if unsafe {
		return "loose"
	}
	return "strict"
}
