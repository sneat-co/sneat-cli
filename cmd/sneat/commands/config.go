package commands

import (
	"github.com/sneat-co/sneat-cli/internal/config"
	"github.com/spf13/cobra"
)

// configFromCmd reads the root persistent flags and resolves config.
func configFromCmd(cmd *cobra.Command, getenv func(string) string) config.Config {
	f := cmd.Flags()
	s := func(name string) string { v, _ := f.GetString(name); return v }
	emulator, _ := f.GetBool("emulator")
	return config.Resolve(config.Overrides{
		Project:               s("project"),
		APIKey:                s("api-key"),
		AuthDomain:            s("auth-domain"),
		APIBaseURL:            s("api-base-url"),
		AuthEmulatorHost:      s("auth-emulator"),
		FirestoreEmulatorHost: s("firestore-emulator"),
		Emulator:              emulator,
	}, getenv)
}
