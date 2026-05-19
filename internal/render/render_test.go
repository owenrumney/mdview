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
