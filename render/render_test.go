package render

import (
	"strings"
	"testing"
)

func TestPageAllowsEmptyDocument(t *testing.T) {
	html, err := Page(RenderedDocument{Title: "empty.md"}, Options{Theme: ThemeDark})
	if err != nil {
		t.Fatalf("Page() unexpected error: %v", err)
	}
	if strings.Contains(string(html), "Select a markdown file from the sidebar") {
		t.Fatalf("Page() rendered directory empty state for an empty document")
	}
}

func TestChatScriptFocusesInput(t *testing.T) {
	html, err := Page(RenderedDocument{Title: "chat.md", Body: []byte("<h1>Chat</h1>")}, Options{Theme: ThemeDark, Chat: true})
	if err != nil {
		t.Fatalf("Page() unexpected error: %v", err)
	}
	page := string(html)
	for _, want := range []string{"function focusChatInput()", "focus({ preventScroll: true })", "window.MDVIEW_CHAT_DOCUMENT_CHANGED", "chatStorageKey(activeChatFile)", "restoreMessages();\n    focusChatInput();"} {
		if !strings.Contains(page, want) {
			t.Fatalf("chat page missing %q", want)
		}
	}
}

func TestShellShowsEmptyState(t *testing.T) {
	html, err := Shell("docs", Options{Theme: ThemeDark})
	if err != nil {
		t.Fatalf("Shell() unexpected error: %v", err)
	}
	if !strings.Contains(string(html), "Select a markdown file from the sidebar") {
		t.Fatalf("Shell() did not render directory empty state")
	}
}

func TestMarkdownCSSStandsAlone(t *testing.T) {
	for _, want := range []string{".markdown-body", "--code-bg", ".copy-btn"} {
		if !strings.Contains(MarkdownCSS, want) {
			t.Errorf("MarkdownCSS is missing %q, which its own rules need", want)
		}
	}
	// A host page brings its own layout, so none of mdview's furniture may ride
	// along and move it.
	for _, chrome := range []string{".mdview-toc", ".mdview-chat", ".mdview-file-explorer", "\nbody {"} {
		if strings.Contains(MarkdownCSS, chrome) {
			t.Errorf("MarkdownCSS carries page furniture %q", chrome)
		}
	}
}

func TestChromeCSSKeepsThePageFurniture(t *testing.T) {
	for _, want := range []string{".mdview-toc", ".mdview-chat", "--toc-width", `body[data-theme="light"]`} {
		if !strings.Contains(chromeCSS, want) {
			t.Errorf("chromeCSS is missing %q", want)
		}
	}
}

func TestMermaidInitCarriesItsConfig(t *testing.T) {
	tests := []struct {
		name   string
		theme  Theme
		unsafe bool
		want   []string
	}{
		{name: "dark and strict", theme: ThemeDark, want: []string{`MDVIEW_MERMAID_THEME = "dark"`, `MDVIEW_MERMAID_SECURITY = "strict"`}},
		{name: "light and loose", theme: ThemeLight, unsafe: true, want: []string{`MDVIEW_MERMAID_THEME = "default"`, `MDVIEW_MERMAID_SECURITY = "loose"`}},
		{name: "unset theme is dark", theme: "", want: []string{`MDVIEW_MERMAID_THEME = "dark"`}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MermaidInit(tt.theme, tt.unsafe)
			for _, want := range tt.want {
				if !strings.Contains(got, want) {
					t.Errorf("MermaidInit() is missing %q", want)
				}
			}
		})
	}
}

func TestBrowserAssetsAreExported(t *testing.T) {
	if CopyJS == "" || ZoomJS == "" || len(MermaidJS) == 0 {
		t.Error("a host page needs CopyJS, ZoomJS and MermaidJS to be non-empty")
	}
}
