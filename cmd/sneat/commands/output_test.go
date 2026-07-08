package commands

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestWriteJSON_IndentsAndNewline(t *testing.T) {
	var buf bytes.Buffer
	if err := writeJSON(&buf, map[string]string{"a": "b"}); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	want := "{\n  \"a\": \"b\"\n}\n"
	if buf.String() != want {
		t.Fatalf("got %q, want %q", buf.String(), want)
	}
}

func TestWriteTable_Bordered(t *testing.T) {
	var buf bytes.Buffer
	if err := writeTable(&buf, []string{"ID", "TYPE"}, [][]string{{"ao58m", "private"}}); err != nil {
		t.Fatalf("writeTable: %v", err)
	}
	got := buf.String()
	for _, want := range []string{"+", "| ID", "| ao58m ", "private"} {
		if !strings.Contains(got, want) {
			t.Fatalf("table missing %q in:\n%s", want, got)
		}
	}
}

func TestWriteCSV(t *testing.T) {
	var buf bytes.Buffer
	if err := writeCSV(&buf, []string{"ID", "TYPE"}, [][]string{{"ao58m", "private"}}); err != nil {
		t.Fatalf("writeCSV: %v", err)
	}
	if got := buf.String(); got != "ID,TYPE\nao58m,private\n" {
		t.Fatalf("csv = %q", got)
	}
}

func TestWriteYAML_UsesJSONKeys(t *testing.T) {
	var buf bytes.Buffer
	type row struct {
		SpaceID string `json:"spaceID"`
	}
	if err := writeYAML(&buf, row{SpaceID: "ao58m"}); err != nil {
		t.Fatalf("writeYAML: %v", err)
	}
	if got := buf.String(); !strings.Contains(got, "spaceID: ao58m") {
		t.Fatalf("yaml = %q", got)
	}
}

func formatCmd(args ...string) *cobra.Command {
	cmd := &cobra.Command{Use: "x", RunE: func(*cobra.Command, []string) error { return nil }}
	addFormatFlags(cmd)
	cmd.SetArgs(args)
	_ = cmd.Execute()
	return cmd
}

func TestFormatFromCmd(t *testing.T) {
	cases := []struct {
		args []string
		want outFormat
		err  bool
	}{
		{nil, fmtTable, false},
		{[]string{"--json"}, fmtJSON, false},
		{[]string{"--yaml"}, fmtYAML, false},
		{[]string{"--csv"}, fmtCSV, false},
		{[]string{"--format=yaml"}, fmtYAML, false},
		{[]string{"--format=table"}, fmtTable, false},
		{[]string{"--json", "--csv"}, "", true},
		{[]string{"--format=xml"}, "", true},
		{[]string{"--json", "--format=yaml"}, "", true},
	}
	for _, tc := range cases {
		got, err := formatFromCmd(formatCmd(tc.args...))
		if tc.err {
			if err == nil {
				t.Errorf("%v: expected error", tc.args)
			}
			continue
		}
		if err != nil {
			t.Errorf("%v: %v", tc.args, err)
		}
		if got != tc.want {
			t.Errorf("%v: got %q want %q", tc.args, got, tc.want)
		}
	}
}
