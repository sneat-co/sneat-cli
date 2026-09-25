// Package commands: the `sneat action` command family talks to the Action
// Protocol (sneat-ai-backend's /v0/sneatai/*). Every subcommand prints JSON
// by default — this family is agent-facing, not primarily for humans — and
// commands are split across action_*.go files, one command per file.
package commands

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sneat-co/sneat-ai-backend/const4sneatai"
	"github.com/sneat-co/sneat-cli/internal/actionstore"
	"github.com/spf13/cobra"
)

// Action builds the `sneat action` command group.
func Action(env Env) *cobra.Command {
	cmd := &cobra.Command{Use: "action", Short: "Create, amend, validate, commit and inspect Sneat.ai actions"}
	cmd.AddCommand(
		actionNewCmd(env),
		actionAddCmd(env),
		actionShowCmd(env),
		actionValidateCmd(env),
		actionCommitCmd(env),
		actionCancelCmd(env),
		actionListCmd(env),
		actionSchemaCmd(env),
	)
	return cmd
}

// actionsDir resolves the directory client-held drafts live in:
// $SNEAT_CONFIG_DIR/sneat/actions when SNEAT_CONFIG_DIR is set (this is the
// injectable seam tests use), else <os.UserConfigDir>/sneat/actions.
func actionsDir(env Env) (string, error) {
	if d := env.Getenv("SNEAT_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "sneat", "actions"), nil
	}
	return actionstore.DefaultDir(os.UserConfigDir)
}

func newActionStore(env Env) (*actionstore.Store, error) {
	dir, err := actionsDir(env)
	if err != nil {
		return nil, err
	}
	return actionstore.NewStore(dir), nil
}

// randRead reads cryptographically random bytes into b. It is a seam over
// crypto/rand.Read (production default) so newActionID's practically
// unreachable failure path can be exercised in tests.
var randRead = rand.Read

// newActionID generates a client-held action id: "act_" + 8 random lowercase
// base36 characters, matching the const4sneatai.ActionIDPrefix format the
// backend validates (act_xxxxxxxx).
func newActionID() (string, error) {
	const alphabet = "0123456789abcdefghijklmnopqrstuvwxyz"
	b := make([]byte, 8)
	if _, err := randRead(b); err != nil {
		return "", err
	}
	out := make([]byte, 8)
	for i, x := range b {
		out[i] = alphabet[int(x)%len(alphabet)]
	}
	return const4sneatai.ActionIDPrefix + string(out), nil
}

// errNoSuchDraft is returned when an action id names neither a client-held
// draft nor a locally-remembered server-held id.
func errNoSuchDraft(actionID string) error {
	return fmt.Errorf("no local draft or server-held index entry for %s; pass the id 'sneat action new' printed", actionID)
}
