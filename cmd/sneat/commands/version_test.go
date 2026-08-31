package commands

import (
	"bytes"
	"testing"

	"github.com/strongo/buildinfo"
)

func TestVersionCommand_PrintsLong(t *testing.T) {
	info := buildinfo.Info{Name: "sneat", Version: "1.2.3", Commit: "abc123", Date: "2026-07-08"}
	cmd := Version(info)
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "sneat 1.2.3 (abc123) 2026-07-08\n"
	if got := buf.String(); got != want {
		t.Fatalf("output = %q, want %q", got, want)
	}
}
