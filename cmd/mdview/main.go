package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/owenrumney/mdview/internal/browser"
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
	defer cancel()

	if err := newRootCmd().ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var (
		watch  bool
		light  bool
		unsafe bool
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
			opts := render.Options{Theme: theme, Unsafe: unsafe}
			if watch {
				return server.Run(cmd.Context(), path, opts)
			}
			return runOnce(path, opts)
		},
	}
	cmd.Flags().BoolVarP(&watch, "watch", "w", false, "watch the file and reload the browser on changes")
	cmd.Flags().BoolVar(&light, "light", false, "render in light mode (default is dark)")
	cmd.Flags().BoolVar(&unsafe, "unsafe", false, "allow raw HTML in markdown and relaxed mermaid security (only use on trusted files)")
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
	if err := os.WriteFile(out, html, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", out, err)
	}
	fmt.Fprintf(os.Stderr, "rendered %s\n", out)
	return browser.Open(out)
}
