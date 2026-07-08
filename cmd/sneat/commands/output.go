package commands

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// outFormat is a supported output format.
type outFormat string

const (
	fmtTable outFormat = "table"
	fmtJSON  outFormat = "json"
	fmtYAML  outFormat = "yaml"
	fmtCSV   outFormat = "csv"
)

var validFormats = map[string]bool{"table": true, "json": true, "yaml": true, "csv": true}

// addFormatFlags registers the shared output-format flags on a command.
func addFormatFlags(cmd *cobra.Command) {
	f := cmd.PersistentFlags()
	f.String("format", "", "output format: table (default), json, yaml, csv")
	f.Bool("table", false, "output as an ASCII table (shortcut for --format=table)")
	f.Bool("json", false, "output as JSON (shortcut for --format=json)")
	f.Bool("yaml", false, "output as YAML (shortcut for --format=yaml)")
	f.Bool("csv", false, "output as CSV (shortcut for --format=csv)")
}

// formatFromCmd resolves the requested format from --format and the --<format>
// shortcuts, defaulting to table. Conflicting or unknown values are errors.
func formatFromCmd(cmd *cobra.Command) (outFormat, error) {
	chosen := map[string]bool{}
	for _, name := range []string{"table", "json", "yaml", "csv"} {
		if b, err := cmd.Flags().GetBool(name); err == nil && b {
			chosen[name] = true
		}
	}
	if f, err := cmd.Flags().GetString("format"); err == nil && f != "" {
		if !validFormats[f] {
			return "", fmt.Errorf("invalid --format %q (want table, json, yaml, or csv)", f)
		}
		chosen[f] = true
	}
	if len(chosen) > 1 {
		return "", fmt.Errorf("conflicting output format flags; pick one of --format/--table/--json/--yaml/--csv")
	}
	for name := range chosen {
		return outFormat(name), nil
	}
	return fmtTable, nil
}

// output renders data in the command's chosen format. data feeds json/yaml;
// headers+rows feed the table and csv renderers.
func output(cmd *cobra.Command, data any, headers []string, rows [][]string) error {
	f, err := formatFromCmd(cmd)
	if err != nil {
		return err
	}
	w := cmd.OutOrStdout()
	switch f {
	case fmtJSON:
		return writeJSON(w, data)
	case fmtYAML:
		return writeYAML(w, data)
	case fmtCSV:
		return writeCSV(w, headers, rows)
	default:
		return writeTable(w, headers, rows)
	}
}

// writeJSON encodes v as indented JSON (2 spaces) followed by a newline.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// writeYAML encodes v as YAML, routing through JSON first so the emitted keys
// match the JSON tags (yaml.v3 does not read json tags).
func writeYAML(w io.Writer, v any) error {
	j, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var generic any
	if err := json.Unmarshal(j, &generic); err != nil {
		return err
	}
	out, err := yaml.Marshal(generic)
	if err != nil {
		return err
	}
	_, err = w.Write(out)
	return err
}

func writeCSV(w io.Writer, headers []string, rows [][]string) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(headers); err != nil {
		return err
	}
	if err := cw.WriteAll(rows); err != nil {
		return err
	}
	cw.Flush()
	return cw.Error()
}

// writeTable renders a styled, bordered table via lipgloss. Rendering is
// pipe-safe: lipgloss emits plain text (no ANSI) when stdout is not a terminal.
func writeTable(w io.Writer, headers []string, rows [][]string) error {
	header := lipgloss.NewStyle().Padding(0, 1).Bold(true).Foreground(lipgloss.Color("6")) // cyan
	cell := lipgloss.NewStyle().Padding(0, 1)
	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(lipgloss.NewStyle().Faint(true)). // dimmed border
		StyleFunc(func(row, _ int) lipgloss.Style {
			if row == table.HeaderRow {
				return header
			}
			return cell
		}).
		Headers(headers...).
		Rows(rows...)
	_, err := fmt.Fprintln(w, t.Render())
	return err
}

// str renders any scalar cell value as a string ("" for nil).
func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// joinList renders a list-valued cell (string or any slice) comma-separated.
func joinList(v any) string {
	switch xs := v.(type) {
	case []string:
		return strings.Join(xs, ",")
	case []any:
		parts := make([]string, len(xs))
		for i, x := range xs {
			parts[i] = str(x)
		}
		return strings.Join(parts, ",")
	default:
		return str(v)
	}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
