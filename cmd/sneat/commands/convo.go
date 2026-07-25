package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/sneat-co/sneat-bots/extensions/anybot/convo/convodev"
	"github.com/sneat-co/sneat-bots/extensions/anybot/convo/convosetup"
	togdactions "github.com/sneat-co/sneat-bots/extensions/togethered/convoactions"
	"github.com/sneat-co/sneat-bots/platform/convo/convomodel"
	"github.com/sneat-co/sneat-bots/platform/convo/convoruntime"
	"github.com/sneat-co/sneat-go-core/coretypes"
	"github.com/sneat-co/sneat-go-core/facade"
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
		convoCatalogsCmd(),
		convoRouteCmd(),
		convoSayCmd(),
		convoReplayCmd(),
	)
	return cmd
}

// newConvoRuntime constructs the runtime used by all convo subcommands.
//
// Composition comes from convosetup, the host's single registration point, so
// the CLI shows exactly what a bot shows — including extensions added later.
// The previous hand-rolled catalog list silently went stale: it predated both
// Trackus and the registry, so `sneat convo actions` had been omitting a whole
// extension.
//
// NewMockRuntime also seeds the deterministic interpreter with the rules each
// extension declares. That is load-bearing now that catalogs own their own
// grammar — a plain llmmock client understands nothing at all.
func newConvoRuntime() (*convoruntime.Runtime, error) {
	return convosetup.NewMockRuntime(convoServices(), togdStubCatalog())
}

// newConvoRegistry composes the same catalogs without an LLM, for the
// introspection commands that only read what is registered.
func newConvoRegistry() (*convoruntime.Registry, error) {
	return convosetup.NewCatalogRegistry(convoServices(), togdStubCatalog())
}

// togdStubCatalog returns a convoruntime.Catalog for the togethered scope
// with stub execute functions (deterministic summaries from args, no DB).
// Used in sneat-cli where the real facade4togd is not available.
func togdStubCatalog() convoruntime.Catalog {
	stub := func(actionID string) convoruntime.ExecuteFunc {
		return func(_ facade.ContextWithUser, _ coretypes.SpaceID, args map[string]any) (convomodel.ActionResult, error) {
			return convomodel.ActionResult{
				ActionID: actionID,
				Summary:  fmt.Sprintf("[stub] %s args=%v", actionID, args),
			}, nil
		}
	}
	actions := make([]convoruntime.BoundAction, len(togdactions.AllDefs))
	for i, def := range togdactions.AllDefs {
		actions[i] = convoruntime.BoundAction{Def: def, Execute: stub(def.ID)}
	}
	catalog := convoruntime.Catalog{
		ID:      "togethered",
		Title:   "ToGethered (stub)",
		Actions: actions,
	}
	// The deterministic interpreter no longer carries any extension's grammar —
	// each catalog brings its own. Without attaching ToGethered's declared rules
	// the stub would list its actions but understand nothing said to it.
	return catalog.MustWithRules(togdactions.Rules, togdactions.RuleFuncs)
}

// setupSandbox wires the sandbox DB (in-memory by default, OpenVaultDB when
// SNEAT_STORAGE=openvaultdb) with the given space and user, and returns a
// context that resolves it.
//
// The sandbox now hands back a context rather than swapping a global and
// returning a restore func, so the DB is passed explicitly to each call
// instead of being ambient.
func setupSandbox(spaceID coretypes.SpaceID, userID string) (context.Context, error) {
	db, err := resolveSandboxDB()
	if err != nil {
		return nil, err
	}
	// Trackus records against a CONTACT, not a user, so its facade needs the
	// contact-resolution seam bound or every measurement fails. The bots host
	// binds the same Contactus-backed shape; without it "20 push-ups" would
	// error out here with "trackus contact resolver is not configured".
	configureConvoServices()
	ctx, _, err := convodev.SetupSandboxWithDB(db, spaceID, userID)
	if err != nil {
		return nil, err
	}
	// Seed a couple of contacts so participant resolution has something to
	// resolve. Created through the REAL Contactus service rather than written
	// straight to storage, so the sandbox exercises the same path a bot does.
	return ctx, seedSandboxContacts(ctx, userID, string(spaceID))
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
				rows = append(rows, []string{d.ID, d.Summary, confirm, d.Extension})
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
	ctx, err := setupSandbox(coretypes.SpaceID(spaceID), userID)
	if err != nil {
		return fmt.Errorf("sandbox setup: %w", err)
	}

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
		resp, err := rt.HandleText(ctx, req)
		if err != nil {
			return fmt.Errorf("HandleText(%q): %w", msg, err)
		}
		turn := convoTurn{Text: msg, Response: resp}

		if resp.Pending != nil && autoYes {
			req2 := req
			req2.Text = "yes"
			resolution, err := rt.ResolvePending(ctx, req2, *resp.Pending, true)
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
		if _, err := fmt.Fprintf(w, "> %s\n", t.Text); err != nil {
			return err
		}
		if _, err := fmt.Fprintln(w, t.Response.ReplyText); err != nil {
			return err
		}
		for _, ex := range t.Response.Executed {
			if _, err := fmt.Fprintf(w, "  [executed] %s: %s\n", ex.ActionID, ex.Summary); err != nil {
				return err
			}
		}
		if t.Response.Pending != nil {
			if _, err := fmt.Fprintf(w, "  [pending] %s\n", t.Response.Pending.Prompt); err != nil {
				return err
			}
			b, _ := json.MarshalIndent(t.Response.Pending.Call, "    ", "  ")
			if _, err := fmt.Fprintf(w, "    %s\n", string(b)); err != nil {
				return err
			}
		}
		if t.Response.Clarification != nil {
			if _, err := fmt.Fprintf(w, "  [clarification] %s\n", t.Response.Clarification.Question); err != nil {
				return err
			}
		}
		if t.Resolution != nil {
			if _, err := fmt.Fprintf(w, "  [resolved] %s\n", t.Resolution.ReplyText); err != nil {
				return err
			}
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

	ctx, err := setupSandbox(coretypes.SpaceID(spaceID), userID)
	if err != nil {
		return fmt.Errorf("sandbox setup: %w", err)
	}

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
			resolution, err := rt.ResolvePending(ctx, req2, *pending, approved)
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
		resp, err := rt.HandleText(ctx, req)
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
