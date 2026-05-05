package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/owenrumney/mdview/internal/browser"
	"github.com/owenrumney/mdview/internal/pdf"
	"github.com/owenrumney/mdview/internal/render"
	"github.com/owenrumney/mdview/internal/server"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := newRootCmd().ExecuteContext(ctx)
	cancel()
	if err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var (
		watch    bool
		light    bool
		unsafe   bool
		pdfMode  bool
		contents bool
	)

	cmd := &cobra.Command{
		Use:   "mdview <file.md>",
		Short: "Render a markdown file in your browser (GFM + Mermaid + syntax highlighting)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]
			if _, err := os.Stat(path); err != nil {
				return fmt.Errorf("stat %s: %w", path, err)
			}
			if watch && pdfMode {
				return fmt.Errorf("--watch and --pdf cannot be used together")
			}
			theme := render.ThemeDark
			if light {
				theme = render.ThemeLight
			}
			if pdfMode {
				theme = render.ThemeLight
			}
			opts := render.Options{Theme: theme, Unsafe: unsafe, Contents: contents}
			if watch {
				return server.Run(cmd.Context(), path, opts)
			}
			if pdfMode {
				return runPDF(cmd.Context(), path, opts)
			}
			return runOnce(path, opts)
		},
	}
	cmd.Flags().BoolVarP(&watch, "watch", "w", false, "watch the file and reload the browser on changes")
	cmd.Flags().BoolVar(&light, "light", false, "render in light mode (default is dark)")
	cmd.Flags().BoolVar(&unsafe, "unsafe", false, "allow raw HTML in markdown and relaxed mermaid security (only use on trusted files)")
	cmd.Flags().BoolVar(&pdfMode, "pdf", false, "render to PDF (requires Chrome/Chromium/Edge/Brave) and open it")
	cmd.Flags().BoolVarP(&contents, "contents", "c", false, "show a table-of-contents sidebar with header links")
	cmd.Version = fmt.Sprintf("%s (commit %s, built %s)", version, commit, date)
	cmd.SilenceUsage = true
	return cmd
}

func runOnce(path string, opts render.Options) error {
	html, err := render.File(path, opts)
	if err != nil {
		return err
	}
	out, err := render.TempFilePath(path)
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, html, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", out, err)
	}
	fmt.Fprintf(os.Stderr, "rendered %s\n", out)
	return browser.Open(out)
}

func runPDF(ctx context.Context, path string, opts render.Options) error {
	html, err := render.File(path, opts)
	if err != nil {
		return err
	}
	htmlPath, err := render.TempFilePath(path)
	if err != nil {
		return err
	}
	if err := os.WriteFile(htmlPath, html, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", htmlPath, err)
	}
	pdfPath := pdfOutputPath(path)
	if err := pdf.Generate(ctx, htmlPath, pdfPath); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wrote %s\n", pdfPath)
	return browser.Open(pdfPath)
}

func pdfOutputPath(source string) string {
	dir := filepath.Dir(source)
	base := filepath.Base(source)
	ext := filepath.Ext(base)
	name := base[:len(base)-len(ext)] + ".pdf"
	return filepath.Join(dir, name)
}
