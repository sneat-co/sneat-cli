package browserauth

import (
	"fmt"
	"os/exec"
	"runtime"
)

// browserCommand returns the OS-specific command to open url in a browser.
func browserCommand(goos, url string) (name string, args []string, err error) {
	switch goos {
	case "darwin":
		return "open", []string{url}, nil
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", url}, nil
	case "linux":
		return "xdg-open", []string{url}, nil
	default:
		return "", nil, fmt.Errorf("browserauth: unsupported platform %q; open %s manually", goos, url)
	}
}

// goos and startCommand are test seams over the process-launching bits of
// OpenBrowser: the OS name that selects the command, and starting it.
var (
	goos         = runtime.GOOS
	startCommand = func(name string, args ...string) error { return exec.Command(name, args...).Start() }
)

// OpenBrowser opens url in the user's default browser (best-effort per OS).
func OpenBrowser(url string) error {
	name, args, err := browserCommand(goos, url)
	if err != nil {
		return err
	}
	return startCommand(name, args...)
}
