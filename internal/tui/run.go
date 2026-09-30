package tui

import "github.com/strongo/strongo-tui/pkg/nav"

// runShell is a seam over nav.Run so tests can drive Run without a terminal.
var runShell = nav.Run

// Run starts the interactive program on the Spaces screen and blocks until the
// user exits. It requires a real terminal (the caller checks that).
func Run(spaces SpacesReader, contacts ContactsReader, deleter ContactDeleter, uid string) error {
	return runShell(New(spaces, contacts, deleter, uid))
}
