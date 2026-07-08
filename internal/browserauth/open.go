package browserauth

import (
	"fmt"
	"os/exec"
	"runtime"
)

// OpenBrowser opens url in the user's default browser (best-effort per OS).
func OpenBrowser(url string) error {
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name, args = "open", []string{url}
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler", url}
	case "linux":
		name, args = "xdg-open", []string{url}
	default:
		return fmt.Errorf("browserauth: unsupported platform %q; open %s manually", runtime.GOOS, url)
	}
	return exec.Command(name, args...).Start()
}
