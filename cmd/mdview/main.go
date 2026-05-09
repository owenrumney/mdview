package main

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	version     = "dev"
	commit      = "none"
	date        = "unknown"
	openBrowser = browser.Open
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
		htmlMode bool
		force    bool
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
				return runPDF(cmd.Context(), path, opts, force)
			}
			if htmlMode {
				return runHTML(path, opts, force)
			}
			return runOnce(path, opts)
		},
	}
	cmd.Flags().BoolVarP(&watch, "watch", "w", false, "watch the file and reload the browser on changes")
	cmd.Flags().BoolVar(&light, "light", false, "render in light mode (default is dark)")
	cmd.Flags().BoolVar(&unsafe, "unsafe", false, "allow raw HTML in markdown and relaxed mermaid security (only use on trusted files)")
	cmd.Flags().BoolVar(&pdfMode, "pdf", false, "render to PDF (requires Chrome/Chromium/Edge/Brave) and open it")
	cmd.Flags().BoolVar(&htmlMode, "html", false, "render to HTML next to the input file and open it")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing output file when using --pdf or --html")
	cmd.Flags().BoolVarP(&contents, "contents", "c", false, "show a table-of-contents sidebar with header links")
	cmd.MarkFlagsMutuallyExclusive("watch", "pdf", "html")
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
	if err := writeOutputFile(out, html, true); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "rendered %s\n", out)
	return openFile(out)
}

func runPDF(ctx context.Context, path string, opts render.Options, force bool) error {
	pdfPath := pdfOutputPath(path)
	if err := checkOutputPath(pdfPath, force); err != nil {
		return err
	}
	html, err := render.File(path, opts)
	if err != nil {
		return err
	}
	htmlPath, err := render.TempFilePath(path)
	if err != nil {
		return err
	}
	if err := writeOutputFile(htmlPath, html, true); err != nil {
		return err
	}
	tmpPDFPath, cleanup, err := tempOutputPath("out.pdf")
	if err != nil {
		return err
	}
	defer cleanup()
	if err := pdf.Generate(ctx, htmlPath, tmpPDFPath); err != nil {
		return err
	}
	if err := copyOutputFile(tmpPDFPath, pdfPath, force); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wrote %s\n", pdfPath)
	return openFile(pdfPath)
}

func runHTML(path string, opts render.Options, force bool) error {
	htmlPath := htmlOutputPath(path)
	if err := checkOutputPath(htmlPath, force); err != nil {
		return err
	}
	html, err := render.File(path, opts)
	if err != nil {
		return err
	}
	if err := writeOutputFile(htmlPath, html, force); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wrote %s\n", htmlPath)
	return openFile(htmlPath)
}

func pdfOutputPath(source string) string {
	return outputPath(source, ".pdf")
}

func htmlOutputPath(source string) string {
	return outputPath(source, ".html")
}

func outputPath(source, newExt string) string {
	dir := filepath.Dir(source)
	base := filepath.Base(source)
	ext := filepath.Ext(base)
	name := base[:len(base)-len(ext)] + newExt
	return filepath.Join(dir, name)
}

func openFile(path string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve open path %s: %w", path, err)
	}
	return openBrowser(absPath)
}

func tempOutputPath(name string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "mdview-")
	if err != nil {
		return "", nil, fmt.Errorf("create temp output dir: %w", err)
	}
	cleanup := func() {
		_ = os.RemoveAll(dir)
	}
	return filepath.Join(dir, name), cleanup, nil
}

func writeOutputFile(path string, data []byte, force bool) error {
	out, err := createOutputFile(path, force)
	if err != nil {
		return err
	}
	success := false
	defer func() {
		if !success {
			_ = os.Remove(path)
		}
	}()
	if _, err := out.Write(data); err != nil {
		_ = out.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close %s: %w", path, err)
	}
	success = true
	return nil
}

func copyOutputFile(source, dest string, force bool) error {
	in, err := os.Open(source) // #nosec G304 -- source is an internally created temp file
	if err != nil {
		return fmt.Errorf("open %s: %w", source, err)
	}
	defer func() {
		_ = in.Close()
	}()
	out, err := createOutputFile(dest, force)
	if err != nil {
		return err
	}
	success := false
	defer func() {
		if !success {
			_ = os.Remove(dest)
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return fmt.Errorf("write %s: %w", dest, err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close %s: %w", dest, err)
	}
	success = true
	return nil
}

func createOutputFile(path string, force bool) (*os.File, error) {
	if force {
		if err := removeExistingOutput(path); err != nil {
			return nil, err
		}
	} else if err := checkOutputPath(path, false); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- path is an intentional CLI output path
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			if force {
				return nil, fmt.Errorf("create %s: output already exists", path)
			}
			return nil, existingOutputError(path)
		}
		return nil, fmt.Errorf("create %s: %w", path, err)
	}
	return f, nil
}

func checkOutputPath(path string, force bool) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check output %s: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("output %s is a directory", path)
	}
	if !force {
		return existingOutputError(path)
	}
	return nil
}

func removeExistingOutput(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check output %s: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("output %s is a directory", path)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("remove existing output %s: %w", path, err)
	}
	return nil
}

func existingOutputError(path string) error {
	return fmt.Errorf("output %s already exists; use --force to overwrite", path)
}
