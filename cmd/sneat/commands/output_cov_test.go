package commands

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestOutputJSONDefault_FormatError(t *testing.T) {
	cmd := formatCmd("--json", "--csv") // conflicting flags -> formatFromCmd errors
	if err := outputJSONDefault(cmd, nil, nil, nil); err == nil {
		t.Fatal("expected format conflict error")
	}
}

func TestOutput_FormatError(t *testing.T) {
	cmd := formatCmd("--format=xml") // invalid format -> formatFromCmd errors
	if err := output(cmd, nil, nil, nil); err == nil {
		t.Fatal("expected invalid format error")
	}
}

func TestRenderOutput_YAML(t *testing.T) {
	var buf bytes.Buffer
	type row struct {
		Name string `json:"name"`
	}
	if err := renderOutput(&buf, fmtYAML, row{Name: "vaoyj"}, nil, nil); err != nil {
		t.Fatalf("renderOutput: %v", err)
	}
	if !strings.Contains(buf.String(), "name: vaoyj") {
		t.Fatalf("output = %q", buf.String())
	}
}

func TestRenderOutput_CSV(t *testing.T) {
	var buf bytes.Buffer
	if err := renderOutput(&buf, fmtCSV, nil, []string{"ID"}, [][]string{{"ao58m"}}); err != nil {
		t.Fatalf("renderOutput: %v", err)
	}
	if buf.String() != "ID\nao58m\n" {
		t.Fatalf("output = %q", buf.String())
	}
}

func TestWriteYAML_JSONMarshalError(t *testing.T) {
	// A channel is not JSON-marshalable, exercising writeYAML's first error
	// branch with a real, non-contrived failure (no seam needed here).
	if err := writeYAML(&bytes.Buffer{}, make(chan int)); err == nil {
		t.Fatal("expected json.Marshal error")
	}
}

func TestWriteYAML_JSONUnmarshalError(t *testing.T) {
	orig := jsonUnmarshalGeneric
	wantErr := errors.New("unmarshal boom")
	jsonUnmarshalGeneric = func([]byte, any) error { return wantErr }
	t.Cleanup(func() { jsonUnmarshalGeneric = orig })

	if err := writeYAML(&bytes.Buffer{}, map[string]string{"a": "b"}); !errors.Is(err, wantErr) {
		t.Fatalf("writeYAML error = %v, want %v", err, wantErr)
	}
}

func TestWriteYAML_YAMLMarshalError(t *testing.T) {
	orig := yamlMarshalFn
	wantErr := errors.New("yaml marshal boom")
	yamlMarshalFn = func(any) ([]byte, error) { return nil, wantErr }
	t.Cleanup(func() { yamlMarshalFn = orig })

	if err := writeYAML(&bytes.Buffer{}, map[string]string{"a": "b"}); !errors.Is(err, wantErr) {
		t.Fatalf("writeYAML error = %v, want %v", err, wantErr)
	}
}

// csvFailWriter always fails its Write call. encoding/csv.Writer wraps a
// bufio.Writer, so a small record never actually reaches the underlying
// io.Writer until a flush -- writeCSV's header-write branch is instead
// exercised by a header field bigger than bufio's internal buffer, which
// forces cw.Write(headers) itself to flush (and fail) immediately.
type csvFailWriter struct{ err error }

func (f *csvFailWriter) Write([]byte) (int, error) { return 0, f.err }

func TestWriteCSV_HeaderWriteError(t *testing.T) {
	wantErr := errors.New("header write boom")
	w := &csvFailWriter{err: wantErr}
	bigHeader := strings.Repeat("H", 8192) // forces bufio to flush mid-Write
	if err := writeCSV(w, []string{bigHeader}, nil); !errors.Is(err, wantErr) {
		t.Fatalf("writeCSV error = %v, want %v", err, wantErr)
	}
}

func TestWriteCSV_RowsWriteError(t *testing.T) {
	wantErr := errors.New("rows write boom")
	w := &csvFailWriter{err: wantErr}
	// Small header+rows stay buffered through Write/WriteAll's loop; the
	// failure only surfaces at WriteAll's trailing Flush, which is still
	// this branch (writeCSV's cw.WriteAll(rows) error check).
	err := writeCSV(w, []string{"ID"}, [][]string{{"a"}, {"b"}})
	if !errors.Is(err, wantErr) {
		t.Fatalf("writeCSV error = %v, want %v", err, wantErr)
	}
}

func TestJoinList_StringSlice(t *testing.T) {
	if got := joinList([]string{"member", "admin"}); got != "member,admin" {
		t.Fatalf("joinList = %q", got)
	}
}

func TestJoinList_AnySlice(t *testing.T) {
	if got := joinList([]any{"member", 42, nil}); got != "member,42," {
		t.Fatalf("joinList = %q", got)
	}
}

func TestJoinList_Default(t *testing.T) {
	if got := joinList("solo"); got != "solo" {
		t.Fatalf("joinList = %q", got)
	}
}
