package browser

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

func Open(url string) error {
	var cmd *exec.Cmd
	if browser := strings.TrimSpace(os.Getenv("MDVIEW_BROWSER")); browser != "" {
		if runtime.GOOS == "darwin" {
			cmd = exec.Command("open", "-a", browser, url) // #nosec G204,G702 -- browser is explicit user configuration
		} else {
			cmd = exec.Command(browser, url) // #nosec G204,G702 -- browser is explicit user configuration
		}
	} else {
		switch runtime.GOOS {
		case "darwin":
			cmd = exec.Command("open", url) // #nosec G204 -- url is internally constructed
		case "windows":
			cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url) // #nosec G204 -- url is internally constructed
		default:
			cmd = exec.Command("xdg-open", url) // #nosec G204 -- url is internally constructed
		}
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}
	return nil
}
