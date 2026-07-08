package commands

import (
	"bytes"
	"testing"
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
