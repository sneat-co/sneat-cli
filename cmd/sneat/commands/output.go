package commands

import (
	"encoding/json"
	"io"
)

// writeJSON encodes v as indented JSON (2 spaces) followed by a newline.
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
