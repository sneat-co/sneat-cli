package main

import (
	"fmt"
	"os"
	"time"

	"github.com/sneat-co/sneat-cli/cmd/sneat/commands"
	"github.com/sneat-co/sneat-cli/internal/browserauth"
	"github.com/sneat-co/sneat-cli/internal/config"
	"github.com/sneat-co/sneat-cli/internal/session"
	"github.com/sneat-co/sneat-cli/internal/sneatauth"
)

// Build metadata, overridable via -ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	path, err := session.DefaultPath(os.UserConfigDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "sneat:", err)
		os.Exit(1)
	}
	env := commands.Env{
		Getenv: os.Getenv,
		Now:    time.Now,
		Store:  session.NewStore(path),
		NewAuthClient: func(cfg config.Config) commands.AuthClient {
			return sneatauth.New(sneatauth.Options{APIKey: cfg.APIKey, AuthEmulatorHost: cfg.AuthEmulatorHost})
		},
		NewBrowserFlow: func(cfg config.Config) commands.BrowserFlow {
			return browserauth.Flow{
				APIKey:           cfg.APIKey,
				AuthDomain:       cfg.AuthDomain,
				Project:          cfg.Project,
				AuthEmulatorHost: cfg.AuthEmulatorHost,
				OpenBrowser:      browserauth.OpenBrowser,
			}
		},
	}
	root := commands.Root(env)
	root.AddCommand(
		commands.Version(version, commit, date),
		commands.Auth(env),
		commands.Whoami(env),
	)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "sneat:", err)
		os.Exit(1)
	}
}
