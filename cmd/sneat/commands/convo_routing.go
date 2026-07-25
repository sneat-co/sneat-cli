package commands

import (
	"fmt"
	"strings"

	"github.com/sneat-co/sneat-bots/platform/convo/convoruntime"
	"github.com/sneat-co/sneat-go-core/convospec"
	"github.com/spf13/cobra"
)

// convoCatalogsCmd reports what the runtime is composed of: which extensions are
// registered and what vocabulary each is listening for.
//
// The point of extension-declared capability is that this is answerable by the
// system rather than by reading composition source, and this is where a person
// asks it.
func convoCatalogsCmd() *cobra.Command {
	var scope []string
	cmd := &cobra.Command{
		Use:   "catalogs",
		Short: "List registered extensions and the vocabulary each listens for",
		RunE: func(cmd *cobra.Command, _ []string) error {
			registry, err := newConvoRegistry()
			if err != nil {
				return err
			}
			described := registry.Describe(scope)
			headers := []string{"CATALOG", "ACTIONS", "TRIGGERS"}
			rows := make([][]string, 0, len(described))
			for _, c := range described {
				triggers := strings.Join(c.Triggers, ", ")
				if triggers == "" {
					// Worth stating rather than leaving blank: a catalog with no
					// triggers can never narrow, so it is only ever reached
					// through the unnarrowed action set.
					triggers = "(none — never narrows)"
				}
				rows = append(rows, []string{c.ID, fmt.Sprintf("%d", len(c.ActionIDs)), triggers})
			}
			if err := writeTable(cmd.OutOrStdout(), headers, rows); err != nil {
				return err
			}
			return reportTriggerHealth(cmd, registry, scope)
		},
	}
	cmd.Flags().StringSliceVar(&scope, "scope", nil, "comma-separated catalog IDs (default: all)")
	return cmd
}

// convoRouteCmd answers "which extension would take this message, and why".
//
// This is the question a misroute investigation actually asks, and without it
// the only way to find out is to read the trigger declarations by hand.
func convoRouteCmd() *cobra.Command {
	var scope []string
	cmd := &cobra.Command{
		Use:   "route <message>",
		Short: "Show which catalog a message's declared triggers select",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			registry, err := newConvoRegistry()
			if err != nil {
				return err
			}
			text := args[0]
			normalized := convospec.NormalizeText(text)
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "message   : %q\n", text)
			fmt.Fprintf(out, "normalized: %q\n\n", normalized)

			var matched []string
			for _, c := range registry.Catalogs(scope) {
				if c.MatchesTriggers(normalized) {
					matched = append(matched, c.ID)
				}
			}

			switch len(matched) {
			case 0:
				fmt.Fprintln(out, "No declared trigger matched, so the prefilter FAILS OPEN:")
				fmt.Fprintln(out, "every action in scope is offered and the model decides.")
			case 1:
				fmt.Fprintf(out, "Exactly one catalog claims this, so the prefilter narrows to it:\n  %s\n", matched[0])
			default:
				fmt.Fprintf(out, "%d catalogs claim this (%s), so the prefilter FAILS OPEN:\n",
					len(matched), strings.Join(matched, ", "))
				fmt.Fprintln(out, "every action in scope is offered rather than one winning by ordering.")
			}
			return nil
		},
	}
	cmd.Flags().StringSliceVar(&scope, "scope", nil, "comma-separated catalog IDs (default: all)")
	return cmd
}

// reportTriggerHealth surfaces overlaps between registered catalogs. Neither is
// an error — the prefilter fails open on both — but each permanently costs the
// narrowing for the words involved, which is worth seeing.
func reportTriggerHealth(cmd *cobra.Command, registry *convoruntime.Registry, scope []string) error {
	out := cmd.OutOrStdout()
	if conflicts := registry.TriggerConflicts(scope); len(conflicts) > 0 {
		fmt.Fprintf(out, "\n%d trigger conflict(s) — these words can never narrow:\n", len(conflicts))
		for _, c := range conflicts {
			fmt.Fprintf(out, "  %s\n", c)
		}
	}
	if shadowed := registry.ShadowedTriggers(scope); len(shadowed) > 0 {
		fmt.Fprintf(out, "\n%d shadowed trigger(s) — a shorter trigger elsewhere matches everything these do:\n", len(shadowed))
		for _, c := range shadowed {
			fmt.Fprintf(out, "  %s\n", c)
		}
	}
	return nil
}
