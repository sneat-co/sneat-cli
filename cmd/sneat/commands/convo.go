package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/sneat-co/sneat-bots/pkg/convo/convodev"
	"github.com/sneat-co/sneat-bots/pkg/convo/convollm/llmmock"
	"github.com/sneat-co/sneat-bots/pkg/convo/convomodel"
	"github.com/sneat-co/sneat-bots/pkg/convo/convoruntime"
	"github.com/sneat-co/sneat-bots/pkg/convo/convosetup"
	"github.com/sneat-co/sneat-go-core/coretypes"
	"github.com/spf13/cobra"
)

// Convo builds the `sneat convo` command group.
func Convo(_ Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "convo",
		Short: "Conversational runtime dev/agent commands",
	}
	cmd.AddCommand(
		convoActionsCmd(),
		convoSayCmd(),
		convoReplayCmd(),
	)
	return cmd
}

// newConvoRuntime constructs the runtime used by all convo subcommands.
func newConvoRuntime() (*convoruntime.Runtime, error) {
	return convosetup.NewRuntime(llmmock.NewClient())
}

// setupSandbox wires the sandbox DB (in-memory by default, OpenVaultDB when
// SNEAT_STORAGE=openvaultdb) with the given space and user.
// The returned restore func must be deferred by the caller.
func setupSandbox(spaceID coretypes.SpaceID, userID string) (func(), error) {
	db, err := resolveSandboxDB()
	if err != nil {
		return nil, err
	}
	_, restore, err := convodev.SetupSandboxWithDB(db, spaceID, userID)
	return restore, err
}

// convoActionsCmd returns the `sneat convo actions` subcommand.
func convoActionsCmd() *cobra.Command {
	var scope []string
	cmd := &cobra.Command{
		Use:   "actions",
		Short: "List available conversational action definitions",
		RunE: func(cmd *cobra.Command, _ []string) error {
			rt, err := newConvoRuntime()
			if err != nil {
				return err
			}
			useJSON, _ := cmd.Flags().GetBool("json")
			if useJSON {
				b, err := rt.SpecJSON(scope)
				if err != nil {
					return err
				}
				_, err = fmt.Fprintln(cmd.OutOrStdout(), string(b))
				return err
			}
			defs := rt.ActionDefs(scope)
			headers := []string{"ID", "SUMMARY", "CONFIRM", "EXTENSION"}
			rows := make([][]string, 0, len(defs))
			for _, d := range defs {
				confirm := "-"
				if d.Confirm {
					confirm = "yes"
				}
				rows = append(rows, []string{d.ID, d.Summary, confirm, string(d.Extension)})
			}
			return writeTable(cmd.OutOrStdout(), headers, rows)
		},
	}
	cmd.Flags().StringSliceVar(&scope, "scope", nil, "comma-separated catalog IDs to include (default: all)")
	cmd.Flags().Bool("json", false, "output action spec as JSON")
	return cmd
}

// convoTurn is the JSON-serializable record of one conversation turn.
type convoTurn struct {
	Text       string               `json:"text"`
	Response   convomodel.Response  `json:"response"`
	Resolution *convomodel.Response `json:"resolution,omitempty"`
}

// convoSayCmd returns the `sneat convo say` subcommand.
func convoSayCmd() *cobra.Command {
	var (
		scope   []string
		autoYes bool
		useJSON bool
		spaceID string
		userID  string
	)
	cmd := &cobra.Command{
		Use:   "say <message> [<message2> ...]",
		Short: "Send one or more messages to the conversational runtime",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSaySession(cmd, args, scope, spaceID, userID, autoYes, useJSON)
		},
	}
	cmd.Flags().StringSliceVar(&scope, "scope", nil, "comma-separated catalog IDs (default: all)")
	cmd.Flags().BoolVar(&autoYes, "yes", false, "auto-approve pending actions")
	cmd.Flags().BoolVar(&useJSON, "json", false, "output as JSON array")
	cmd.Flags().StringVar(&spaceID, "space", "space1", "sandbox space ID")
	cmd.Flags().StringVar(&userID, "user", "user1", "sandbox user ID")
	return cmd
}

// runSaySession wires the sandbox, creates the runtime and processes messages.
func runSaySession(cmd *cobra.Command, messages []string, scope []string, spaceID, userID string, autoYes, useJSON bool) error {
	restore, err := setupSandbox(coretypes.SpaceID(spaceID), userID)
	if err != nil {
		return fmt.Errorf("sandbox setup: %w", err)
	}
	defer restore()

	rt, err := newConvoRuntime()
	if err != nil {
		return err
	}

	req := convomodel.Request{
		UserID:  userID,
		SpaceID: coretypes.SpaceID(spaceID),
		Scope:   scope,
	}

	var turns []convoTurn

	for _, msg := range messages {
		req.Text = msg
		resp, err := rt.HandleText(cmd.Context(), req)
		if err != nil {
			return fmt.Errorf("HandleText(%q): %w", msg, err)
		}
		turn := convoTurn{Text: msg, Response: resp}

		if resp.Pending != nil && autoYes {
			req2 := req
			req2.Text = "yes"
			resolution, err := rt.ResolvePending(cmd.Context(), req2, *resp.Pending, true)
			if err != nil {
				return fmt.Errorf("ResolvePending: %w", err)
			}
			turn.Resolution = &resolution
		}

		turns = append(turns, turn)
	}

	if useJSON {
		return writeJSON(cmd.OutOrStdout(), turns)
	}
	return printTurns(cmd, turns)
}

// printTurns writes human-readable multi-turn output.
func printTurns(cmd *cobra.Command, turns []convoTurn) error {
	w := cmd.OutOrStdout()
	for _, t := range turns {
		fmt.Fprintf(w, "> %s\n", t.Text)
		fmt.Fprintln(w, t.Response.ReplyText)
		for _, ex := range t.Response.Executed {
			fmt.Fprintf(w, "  [executed] %s: %s\n", ex.ActionID, ex.Summary)
		}
		if t.Response.Pending != nil {
			fmt.Fprintf(w, "  [pending] %s\n", t.Response.Pending.Prompt)
			b, _ := json.MarshalIndent(t.Response.Pending.Call, "    ", "  ")
			fmt.Fprintf(w, "    %s\n", string(b))
		}
		if t.Response.Clarification != nil {
			fmt.Fprintf(w, "  [clarification] %s\n", t.Response.Clarification.Question)
		}
		if t.Resolution != nil {
			fmt.Fprintf(w, "  [resolved] %s\n", t.Resolution.ReplyText)
		}
	}
	return nil
}

// convoReplayCmd returns the `sneat convo replay` subcommand.
func convoReplayCmd() *cobra.Command {
	var (
		scope   []string
		useJSON bool
		spaceID string
		userID  string
	)
	cmd := &cobra.Command{
		Use:   "replay <file>",
		Short: "Replay a conversation script file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runReplaySession(cmd, args[0], scope, spaceID, userID, useJSON)
		},
	}
	cmd.Flags().StringSliceVar(&scope, "scope", nil, "comma-separated catalog IDs (default: all)")
	cmd.Flags().BoolVar(&useJSON, "json", false, "output as JSON array")
	cmd.Flags().StringVar(&spaceID, "space", "space1", "sandbox space ID")
	cmd.Flags().StringVar(&userID, "user", "user1", "sandbox user ID")
	return cmd
}

// runReplaySession reads a script file and replays the conversation.
func runReplaySession(cmd *cobra.Command, scriptFile string, scope []string, spaceID, userID string, useJSON bool) error {
	data, err := os.ReadFile(scriptFile)
	if err != nil {
		return fmt.Errorf("reading script file: %w", err)
	}

	restore, err := setupSandbox(coretypes.SpaceID(spaceID), userID)
	if err != nil {
		return fmt.Errorf("sandbox setup: %w", err)
	}
	defer restore()

	rt, err := newConvoRuntime()
	if err != nil {
		return err
	}

	req := convomodel.Request{
		UserID:  userID,
		SpaceID: coretypes.SpaceID(spaceID),
		Scope:   scope,
	}

	var turns []convoTurn
	var pending *convomodel.PendingAction

	for _, rawLine := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		normalized := strings.ToLower(line)
		if normalized == "yes" || normalized == "no" {
			if pending == nil {
				return fmt.Errorf("script has %q but no pending action", line)
			}
			approved := normalized == "yes"
			req2 := req
			req2.Text = line
			resolution, err := rt.ResolvePending(cmd.Context(), req2, *pending, approved)
			if err != nil {
				return fmt.Errorf("ResolvePending(%q): %w", line, err)
			}
			// Attach resolution to the last turn
			if len(turns) > 0 {
				turns[len(turns)-1].Resolution = &resolution
			}
			pending = nil
			continue
		}

		req.Text = line
		resp, err := rt.HandleText(cmd.Context(), req)
		if err != nil {
			return fmt.Errorf("HandleText(%q): %w", line, err)
		}
		if resp.Pending != nil {
			pending = resp.Pending
		} else {
			pending = nil
		}
		turns = append(turns, convoTurn{Text: line, Response: resp})
	}

	if useJSON {
		return writeJSON(cmd.OutOrStdout(), turns)
	}
	return printTurns(cmd, turns)
}
