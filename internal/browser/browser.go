package browser

import (
	"fmt"
	"os/exec"
	"runtime"
)

func Open(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url) // #nosec G204 -- url is internally constructed
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url) // #nosec G204 -- url is internally constructed
	default:
		cmd = exec.Command("xdg-open", url) // #nosec G204 -- url is internally constructed
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}
	return nil
}
