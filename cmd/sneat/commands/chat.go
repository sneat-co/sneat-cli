package commands

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Chat is the top-level `sneat chat` — launches the interactive chat session.
func Chat(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "chat",
		Short: "Chat with your spaces in an interactive terminal session",
		RunE:  func(cmd *cobra.Command, _ []string) error { return runChat(env, cmd) },
	}
}

// runChat checks the startup preconditions — a terminal and a signed-in
// session — then resolves the spaces reader and delegates to env.RunChat.
// Every check runs before any terminal program exists, so the command stays
// testable without a TTY.
func runChat(env Env, cmd *cobra.Command) error {
	if env.IsTerminal != nil && !env.IsTerminal() {
		return fmt.Errorf("the interactive chat requires a terminal")
	}
	cfg := configFromCmd(cmd, env.Getenv)
	sess, err := env.Store.Load()
	if err != nil {
		return err
	}
	spaces, err := env.NewSpacesReader(cfg)
	if err != nil {
		return err
	}
	return env.RunChat(spaces, sess.UID)
}
