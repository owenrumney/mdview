package server

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMarkdownFiles(t *testing.T) {
	dir := t.TempDir()
	for name, data := range map[string]string{
		"b.md":       "# B\n",
		"a.markdown": "# A\n",
		"notes.txt":  "ignore\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "nested.md"), 0o700); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "nested"), 0o700); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "c.md"), []byte("# C\n"), 0o600); err != nil {
		t.Fatalf("write nested/c.md: %v", err)
	}

	got, err := markdownFiles(dir)
	if err != nil {
		t.Fatalf("markdownFiles() error = %v", err)
	}
	want := []string{"a.markdown", "b.md", "nested/c.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("markdownFiles() = %#v, want %#v", got, want)
	}
}

func TestServedDocumentWithFreshFilesPicksUpNewMarkdown(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "new.md"), []byte("# New\n"), 0o600); err != nil {
		t.Fatalf("write markdown: %v", err)
	}
	doc := servedDocument{root: dir, dir: true}

	fresh, err := doc.withFreshFiles()
	if err != nil {
		t.Fatalf("withFreshFiles() error = %v", err)
	}
	path, current, err := fresh.selectedFile("new.md")
	if err != nil {
		t.Fatalf("selectedFile() error = %v", err)
	}
	if current != "new.md" || path != filepath.Join(dir, "new.md") {
		t.Fatalf("selectedFile() = %q, %q; want new.md", path, current)
	}
}

func TestServedDocumentSelectedAllowsNestedMarkdown(t *testing.T) {
	doc := servedDocument{root: t.TempDir(), dir: true, files: []string{"nested/a.md"}}
	req := httptest.NewRequest("GET", "/?file=nested/a.md", nil)

	path, current, err := doc.selected(req)
	if err != nil {
		t.Fatalf("selected() error = %v", err)
	}
	if current != "nested/a.md" || path != filepath.Join(doc.root, "nested", "a.md") {
		t.Fatalf("selected() = %q, %q; want nested/a.md", path, current)
	}
}

func TestServedDocumentSelectedRejectsTraversal(t *testing.T) {
	doc := servedDocument{root: t.TempDir(), dir: true, files: []string{"a.md"}}
	req := httptest.NewRequest("GET", "/?file=../secret.md", nil)

	_, _, err := doc.selected(req)
	if err == nil {
		t.Fatal("selected() error = nil, want unknown file error")
	}
}
