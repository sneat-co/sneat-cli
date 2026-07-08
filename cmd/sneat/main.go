package main

import (
	"os"
	"time"

	"github.com/sneat-co/sneat-cli/cmd/sneat/commands"
)

// Build metadata, overridable via -ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	env := commands.Env{Getenv: os.Getenv, Now: time.Now}
	root := commands.Root(env)
	root.AddCommand(commands.Version(version, commit, date))
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
