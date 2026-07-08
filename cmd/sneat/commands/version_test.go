package commands

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestVersionCommand_PrintsJSON(t *testing.T) {
	cmd := Version("1.2.3", "abc123", "2026-07-08")
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}
	var got map[string]string
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output not JSON: %v (%q)", err, buf.String())
	}
	if got["version"] != "1.2.3" {
		t.Fatalf("version = %q, want 1.2.3", got["version"])
	}
}
