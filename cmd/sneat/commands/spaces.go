package commands

import (
	"context"

	"github.com/spf13/cobra"
)

// SpacesReader lists the signed-in user's spaces.
type SpacesReader interface {
	ListSpaces(ctx context.Context, uid string) (map[string]any, error)
}

// Space builds the `sneat space` command group. Bare `sneat space` shows help;
// the plural `sneat spaces` (see Spaces) is the alias for `sneat space list`.
func Space(env Env) *cobra.Command {
	cmd := &cobra.Command{Use: "space", Short: "Work with your spaces"}
	cmd.AddCommand(spaceList(env), spaceUse(env), spaceCurrent(env))
	return cmd
}

func spaceUse(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "use <space>",
		Short: "Set the current space (a space id, or 'family'/'private')",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sess, err := env.Store.Load()
			if err != nil {
				return err
			}
			sess.CurrentSpace = args[0]
			if err := env.Store.Save(sess); err != nil {
				return err
			}
			return outputCurrentSpace(cmd, args[0])
		},
	}
}

func spaceCurrent(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Show the current space",
		RunE: func(cmd *cobra.Command, _ []string) error {
			sess, err := env.Store.Load()
			if err != nil {
				return err
			}
			return outputCurrentSpace(cmd, sess.CurrentSpace)
		},
	}
}

func outputCurrentSpace(cmd *cobra.Command, space string) error {
	return output(cmd, map[string]string{"currentSpace": space}, []string{"CURRENT SPACE"}, [][]string{{space}})
}

// Spaces is the top-level `sneat spaces` — an alias for `sneat space list`.
func Spaces(env Env) *cobra.Command {
	return &cobra.Command{
		Use:    "spaces",
		Short:  "List your spaces (alias for 'space list')",
		Hidden: true,
		RunE:   func(cmd *cobra.Command, _ []string) error { return runSpaceList(env, cmd) },
	}
}

func spaceList(env Env) *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List your spaces as JSON",
		RunE:  func(cmd *cobra.Command, _ []string) error { return runSpaceList(env, cmd) },
	}
}

func runSpaceList(env Env, cmd *cobra.Command) error {
	cfg := configFromCmd(cmd, env.Getenv)
	sess, err := env.Store.Load()
	if err != nil {
		return err
	}
	reader, err := env.NewSpacesReader(cfg)
	if err != nil {
		return err
	}
	spaces, err := reader.ListSpaces(cmd.Context(), sess.UID)
	if err != nil {
		return err
	}
	headers, rows := spacesTable(spaces)
	return output(cmd, spaces, headers, rows)
}

func spacesTable(spaces map[string]any) (headers []string, rows [][]string) {
	headers = []string{"ID", "TITLE", "TYPE", "STATUS", "ROLES"}
	for _, id := range sortedKeys(spaces) {
		b, _ := spaces[id].(map[string]any)
		rows = append(rows, []string{id, str(b["title"]), str(b["type"]), str(b["status"]), joinList(b["roles"])})
	}
	return headers, rows
}
