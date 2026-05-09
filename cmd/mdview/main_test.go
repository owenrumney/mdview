package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/owenrumney/mdview/internal/render"
)

func TestOutputPaths(t *testing.T) {
	tests := []struct {
		name string
		src  string
		pdf  string
		html string
	}{
		{
			name: "markdown file",
			src:  "notes.md",
			pdf:  "notes.pdf",
			html: "notes.html",
		},
		{
			name: "nested markdown file",
			src:  filepath.Join("docs", "guide.markdown"),
			pdf:  filepath.Join("docs", "guide.pdf"),
			html: filepath.Join("docs", "guide.html"),
		},
		{
			name: "no extension",
			src:  "README",
			pdf:  "README.pdf",
			html: "README.html",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pdfOutputPath(tt.src); got != tt.pdf {
				t.Fatalf("pdfOutputPath() = %q, want %q", got, tt.pdf)
			}
			if got := htmlOutputPath(tt.src); got != tt.html {
				t.Fatalf("htmlOutputPath() = %q, want %q", got, tt.html)
			}
		})
	}
}

func TestWriteOutputFileForceBehavior(t *testing.T) {
	tests := []struct {
		name     string
		existing bool
		force    bool
		wantErr  string
		wantData string
	}{
		{
			name:     "new file without force",
			existing: false,
			force:    false,
			wantData: "new",
		},
		{
			name:     "new file with force",
			existing: false,
			force:    true,
			wantData: "new",
		},
		{
			name:     "existing file without force",
			existing: true,
			force:    false,
			wantErr:  "already exists; use --force",
			wantData: "old",
		},
		{
			name:     "existing file with force",
			existing: true,
			force:    true,
			wantData: "new",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "out.html")
			if tt.existing {
				if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
					t.Fatalf("write existing output: %v", err)
				}
			}

			err := writeOutputFile(path, []byte("new"), tt.force)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("writeOutputFile() error = nil, want %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("writeOutputFile() error = %q, want to contain %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("writeOutputFile() unexpected error: %v", err)
			}

			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read output: %v", err)
			}
			if string(got) != tt.wantData {
				t.Fatalf("output data = %q, want %q", got, tt.wantData)
			}
		})
	}
}

func TestWriteOutputFileDoesNotReplaceDirectory(t *testing.T) {
	tests := []struct {
		name  string
		force bool
	}{
		{name: "without force", force: false},
		{name: "with force", force: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "out.html")
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatalf("create output directory: %v", err)
			}

			err := writeOutputFile(path, []byte("new"), tt.force)
			if err == nil {
				t.Fatal("writeOutputFile() error = nil, want directory error")
			}
			if !strings.Contains(err.Error(), "is a directory") {
				t.Fatalf("writeOutputFile() error = %q, want directory error", err)
			}

			info, err := os.Stat(path)
			if err != nil {
				t.Fatalf("stat output directory: %v", err)
			}
			if !info.IsDir() {
				t.Fatal("output directory was replaced")
			}
		})
	}
}

func TestWriteOutputFileSymlink(t *testing.T) {
	tests := []struct {
		name           string
		force          bool
		wantErr        string
		wantSymlink    bool
		wantOutputData string
	}{
		{
			name:        "without force",
			force:       false,
			wantErr:     "already exists; use --force",
			wantSymlink: true,
		},
		{
			name:           "with force",
			force:          true,
			wantOutputData: "new",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "target.txt")
			path := filepath.Join(dir, "out.html")
			if err := os.WriteFile(target, []byte("target"), 0o600); err != nil {
				t.Fatalf("write symlink target: %v", err)
			}
			if err := os.Symlink(target, path); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}

			err := writeOutputFile(path, []byte("new"), tt.force)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("writeOutputFile() error = nil, want %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("writeOutputFile() error = %q, want to contain %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("writeOutputFile() unexpected error: %v", err)
			}

			targetData, err := os.ReadFile(target)
			if err != nil {
				t.Fatalf("read symlink target: %v", err)
			}
			if string(targetData) != "target" {
				t.Fatalf("symlink target data = %q, want %q", targetData, "target")
			}

			info, err := os.Lstat(path)
			if err != nil {
				t.Fatalf("lstat output: %v", err)
			}
			gotSymlink := info.Mode()&os.ModeSymlink != 0
			if gotSymlink != tt.wantSymlink {
				t.Fatalf("output symlink = %v, want %v", gotSymlink, tt.wantSymlink)
			}
			if tt.wantOutputData != "" {
				got, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read output: %v", err)
				}
				if string(got) != tt.wantOutputData {
					t.Fatalf("output data = %q, want %q", got, tt.wantOutputData)
				}
			}
		})
	}
}

func TestRootCmdMutuallyExclusiveFlags(t *testing.T) {
	mdPath := filepath.Join(t.TempDir(), "notes.md")
	if err := os.WriteFile(mdPath, []byte("# Notes\n"), 0o600); err != nil {
		t.Fatalf("write markdown: %v", err)
	}
	tests := []struct {
		name string
		args []string
	}{
		{name: "watch and pdf", args: []string{"--watch", "--pdf", mdPath}},
		{name: "watch and html", args: []string{"--watch", "--html", mdPath}},
		{name: "pdf and html", args: []string{"--pdf", "--html", mdPath}},
		{name: "all output modes", args: []string{"--watch", "--pdf", "--html", mdPath}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := newRootCmd()
			cmd.SetArgs(tt.args)
			cmd.SetOut(io.Discard)
			cmd.SetErr(io.Discard)

			err := cmd.Execute()
			if err == nil {
				t.Fatal("Execute() error = nil, want mutual exclusion error")
			}
			if !strings.Contains(err.Error(), "none of the others") {
				t.Fatalf("Execute() error = %q, want mutual exclusion error", err)
			}
		})
	}
}

func TestRunHTML(t *testing.T) {
	tests := []struct {
		name         string
		existingHTML bool
		force        bool
		wantErr      string
		wantOpened   bool
		wantOldHTML  bool
	}{
		{
			name:       "new output",
			wantOpened: true,
		},
		{
			name:         "existing output without force",
			existingHTML: true,
			wantErr:      "already exists; use --force",
			wantOldHTML:  true,
		},
		{
			name:         "existing output with force",
			existingHTML: true,
			force:        true,
			wantOpened:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			mdPath := filepath.Join(dir, "notes.md")
			htmlPath := filepath.Join(dir, "notes.html")
			if err := os.WriteFile(mdPath, []byte("# Hello\n"), 0o600); err != nil {
				t.Fatalf("write markdown: %v", err)
			}
			if tt.existingHTML {
				if err := os.WriteFile(htmlPath, []byte("old"), 0o600); err != nil {
					t.Fatalf("write existing html: %v", err)
				}
			}

			opened := ""
			stubOpenBrowser(t, func(path string) error {
				opened = path
				return nil
			})

			err := runHTML(mdPath, render.Options{Theme: render.ThemeDark}, tt.force)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("runHTML() error = nil, want %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("runHTML() error = %q, want to contain %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("runHTML() unexpected error: %v", err)
			}

			if (opened != "") != tt.wantOpened {
				t.Fatalf("opened = %q, want opened %v", opened, tt.wantOpened)
			}
			if tt.wantOpened {
				wantOpened, err := filepath.Abs(htmlPath)
				if err != nil {
					t.Fatalf("resolve html path: %v", err)
				}
				if opened != wantOpened {
					t.Fatalf("opened = %q, want %q", opened, wantOpened)
				}
			}

			got, err := os.ReadFile(htmlPath)
			if err != nil {
				t.Fatalf("read html output: %v", err)
			}
			if tt.wantOldHTML {
				if string(got) != "old" {
					t.Fatalf("html output = %q, want old content", got)
				}
				return
			}
			if !bytes.Contains(got, []byte("Hello")) {
				t.Fatalf("html output does not contain rendered markdown heading: %q", got)
			}
		})
	}
}

func TestRunPDFRequiresForceForExistingOutput(t *testing.T) {
	dir := t.TempDir()
	mdPath := filepath.Join(dir, "notes.md")
	pdfPath := filepath.Join(dir, "notes.pdf")
	if err := os.WriteFile(mdPath, []byte("# Hello\n"), 0o600); err != nil {
		t.Fatalf("write markdown: %v", err)
	}
	if err := os.WriteFile(pdfPath, []byte("old pdf"), 0o600); err != nil {
		t.Fatalf("write existing pdf: %v", err)
	}

	err := runPDF(context.Background(), mdPath, render.Options{Theme: render.ThemeLight}, false)
	if err == nil {
		t.Fatal("runPDF() error = nil, want existing output error")
	}
	if !strings.Contains(err.Error(), "already exists; use --force") {
		t.Fatalf("runPDF() error = %q, want existing output error", err)
	}

	got, err := os.ReadFile(pdfPath)
	if err != nil {
		t.Fatalf("read existing pdf: %v", err)
	}
	if string(got) != "old pdf" {
		t.Fatalf("existing pdf = %q, want it preserved", got)
	}
}

func TestOpenFileUsesAbsolutePath(t *testing.T) {
	opened := ""
	stubOpenBrowser(t, func(path string) error {
		opened = path
		return nil
	})

	if err := openFile("notes.html"); err != nil {
		t.Fatalf("openFile() unexpected error: %v", err)
	}
	if opened == "" {
		t.Fatal("openBrowser was not called")
	}
	if !filepath.IsAbs(opened) {
		t.Fatalf("opened path = %q, want absolute path", opened)
	}
}

func stubOpenBrowser(t *testing.T, fn func(string) error) {
	t.Helper()
	old := openBrowser
	openBrowser = fn
	t.Cleanup(func() {
		openBrowser = old
	})
}
