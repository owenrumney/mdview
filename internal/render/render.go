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
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	gmhtml "github.com/yuin/goldmark/renderer/html"
)

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
}

func File(path string, opts Options) ([]byte, error) {
	src, err := os.ReadFile(path) // #nosec G304 -- path comes from CLI arg by design
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if opts.Theme == "" {
		opts.Theme = ThemeDark
	}
	title := opts.Title
	if title == "" {
		title = filepath.Base(path)
	}
	body, err := toHTML(src, opts.Theme, opts.Unsafe)
	if err != nil {
		return nil, err
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

func toHTML(src []byte, theme Theme, unsafe bool) ([]byte, error) {
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
		),
		goldmark.WithParserOptions(
			parser.WithAutoHeadingID(),
		),
		goldmark.WithRendererOptions(rendererOpts...),
	)
	var buf bytes.Buffer
	if err := md.Convert(src, &buf); err != nil {
		return nil, fmt.Errorf("convert markdown: %w", err)
	}
	return buf.Bytes(), nil
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
