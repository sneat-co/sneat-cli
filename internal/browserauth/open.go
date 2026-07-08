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

// OpenBrowser opens url in the user's default browser (best-effort per OS).
func OpenBrowser(url string) error {
	name, args, err := browserCommand(runtime.GOOS, url)
	if err != nil {
		return err
	}
	return exec.Command(name, args...).Start()
}
