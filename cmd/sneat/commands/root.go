package commands

import (
	"time"

	"github.com/spf13/cobra"
)

// Env holds injected process dependencies so commands stay unit-testable.
// Extended in later tasks (session store, auth-client factory, browser opener).
type Env struct {
	Getenv func(string) string
	Now    func() time.Time
}

// Root builds the top-level `sneat` command.
func Root(env Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "sneat",
		Short:         "Sneat.app command-line interface",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	// Persistent config flags (resolved in Task 3's configFromCmd helper).
	cmd.PersistentFlags().String("project", "", "Firebase project id (default sneat-eur3-1)")
	cmd.PersistentFlags().String("api-key", "", "Firebase web API key")
	cmd.PersistentFlags().String("api-base-url", "", "sneat-go API base URL")
	cmd.PersistentFlags().String("auth-emulator", "", "Firebase Auth emulator host, e.g. localhost:9099")
	cmd.PersistentFlags().String("firestore-emulator", "", "Firestore emulator host, e.g. localhost:8080")
	cmd.PersistentFlags().Bool("emulator", false, "use local Auth+Firestore emulators on default ports")
	return cmd
}
