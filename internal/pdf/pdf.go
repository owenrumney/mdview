package pdf

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

func Generate(ctx context.Context, htmlPath, outPath string) error {
	bin, err := FindBrowser()
	if err != nil {
		return err
	}
	absHTML, err := filepath.Abs(htmlPath)
	if err != nil {
		return fmt.Errorf("resolve html path: %w", err)
	}
	absOut, err := filepath.Abs(outPath)
	if err != nil {
		return fmt.Errorf("resolve output path: %w", err)
	}
	args := []string{
		"--headless=new",
		"--disable-gpu",
		"--hide-scrollbars",
		"--no-pdf-header-footer",
		"--generate-pdf-document-outline",
		"--export-tagged-pdf",
		"--virtual-time-budget=10000",
		"--run-all-compositor-stages-before-draw",
		"--print-to-pdf=" + absOut,
		"file://" + absHTML,
	}
	runCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(runCtx, bin, args...) // #nosec G204 -- bin is from FindBrowser, args are internally constructed
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("generate pdf via %s: %w (output: %s)", filepath.Base(bin), err, string(out))
	}
	if _, err := os.Stat(absOut); err != nil {
		return fmt.Errorf("pdf not produced at %s: %w", absOut, err)
	}
	return nil
}

func FindBrowser() (string, error) {
	for _, p := range candidatePaths() {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	for _, name := range []string{"google-chrome-stable", "google-chrome", "chromium", "chromium-browser", "chrome", "microsoft-edge", "brave-browser"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, nil
		}
	}
	return "", errors.New("no Chromium-family browser found (install Google Chrome, Chromium, Brave, or Edge to enable --pdf)")
}

func candidatePaths() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		}
	case "windows":
		return []string{
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
			`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
			`C:\Program Files\BraveSoftware\Brave-Browser\Application\brave.exe`,
		}
	default:
		return nil
	}
}
