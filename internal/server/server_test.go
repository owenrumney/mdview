package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/owenrumney/mdview/internal/render"
)

func TestHandleChatRequiresToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(path, []byte("# Notes\n"), 0o600); err != nil {
		t.Fatalf("write markdown: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewBufferString(`{"message":"hello"}`))
	rec := httptest.NewRecorder()

	handleChat(servedDocument{root: path}, "secret", render.Options{}, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestHandleChatRendersReplySafelyWhenDocumentAllowsUnsafeHTML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(path, []byte("# Notes\n"), 0o600); err != nil {
		t.Fatalf("write markdown: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewBufferString(`{"message":"<script>alert(1)</script>"}`))
	req.Header.Set("X-Mdview-Token", "secret")
	rec := httptest.NewRecorder()

	handleChat(servedDocument{root: path}, "secret", render.Options{Unsafe: true}, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var res chatResponse
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if strings.Contains(res.ReplyHTML, "<script>") {
		t.Fatalf("reply HTML contains unsafe script: %q", res.ReplyHTML)
	}
}

func TestHandleChatMockResponse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(path, []byte("# Notes\n"), 0o600); err != nil {
		t.Fatalf("write markdown: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/chat", bytes.NewBufferString(`{"message":"summarise this"}`))
	req.Header.Set("X-Mdview-Token", "secret")
	rec := httptest.NewRecorder()

	handleChat(servedDocument{root: path}, "secret", render.Options{}, nil).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var res chatResponse
	if err := json.NewDecoder(rec.Body).Decode(&res); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !strings.Contains(res.Reply, "Chat UI is wired up") {
		t.Fatalf("reply = %q, want mock backend message", res.Reply)
	}
	if !strings.Contains(res.Reply, "summarise this") {
		t.Fatalf("reply = %q, want echoed message", res.Reply)
	}
}
