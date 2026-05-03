package render

import (
	"html/template"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

var kindMermaidBlock = ast.NewNodeKind("MermaidBlock")

type mermaidBlock struct {
	ast.BaseBlock
	code []byte
}

func (n *mermaidBlock) Dump(source []byte, level int) {
	ast.DumpHelper(n, source, level, nil, nil)
}

func (n *mermaidBlock) Kind() ast.NodeKind { return kindMermaidBlock }

type mermaidExt struct{}

func (e *mermaidExt) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithASTTransformers(
		util.Prioritized(&mermaidTransformer{}, 100),
	))
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(&mermaidRenderer{}, 100),
	))
}

type mermaidTransformer struct{}

func (t *mermaidTransformer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
	var targets []*ast.FencedCodeBlock
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		cb, ok := n.(*ast.FencedCodeBlock)
		if !ok {
			return ast.WalkContinue, nil
		}
		if string(cb.Language(source)) == "mermaid" {
			targets = append(targets, cb)
		}
		return ast.WalkContinue, nil
	})
	for _, cb := range targets {
		var code []byte
		for i := 0; i < cb.Lines().Len(); i++ {
			seg := cb.Lines().At(i)
			code = append(code, seg.Value(source)...)
		}
		mb := &mermaidBlock{code: code}
		parent := cb.Parent()
		parent.ReplaceChild(parent, cb, mb)
	}
}

type mermaidRenderer struct{}

func (r *mermaidRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(kindMermaidBlock, r.render)
}

func (r *mermaidRenderer) render(w util.BufWriter, _ []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	mb := n.(*mermaidBlock)
	_, _ = w.WriteString(`<pre class="mermaid">`)
	template.HTMLEscape(w, mb.code)
	_, _ = w.WriteString("</pre>\n")
	return ast.WalkSkipChildren, nil
}
