package chat

import (
	"net/url"
	"testing"
)

// TestCallbackData_ScenarioFromSpec is the scenario from
// _tests/buttons-use-botkb-and-url-callback-data.md, asserted against the same
// url.Parse contract bots-fw's router applies (botswebhook/router.go: url.Parse
// the callback data, then look the command up by botsfw.CommandCode(URL.Path)).
//
// It parses the literal string rather than our own helper's output on purpose:
// this is the format's contract with the framework, and it must hold even if
// the encoder is rewritten.
func TestCallbackData_ScenarioFromSpec(t *testing.T) {
	const data = "space?id=family1"

	// WHEN it is parsed with url.Parse (exactly what the router does).
	u, err := url.Parse(data)
	if err != nil {
		t.Fatalf("url.Parse(%q) = %v, want no error", data, err)
	}
	// THEN the URL path is `space`.
	if u.Path != "space" {
		t.Errorf("url.Parse(%q).Path = %q, want %q", data, u.Path, "space")
	}
	// AND the query argument `id` is `family1`.
	if got := u.Query().Get("id"); got != "family1" {
		t.Errorf("url.Parse(%q).Query()[id] = %q, want %q", data, got, "family1")
	}

	// The encoder produces exactly that string...
	got, err := encodeCallbackData("space", url.Values{"id": {"family1"}})
	if err != nil {
		t.Fatalf("encodeCallbackData = %v, want no error", err)
	}
	if got != data {
		t.Errorf("encodeCallbackData = %q, want %q", got, data)
	}

	// ...and the parse helper reads it back as command + argument.
	cd, err := parseCallbackData(data)
	if err != nil {
		t.Fatalf("parseCallbackData(%q) = %v, want no error", data, err)
	}
	if cd.command != "space" {
		t.Errorf("command = %q, want %q", cd.command, "space")
	}
	if v, ok := cd.arg("id"); !ok || v != "family1" {
		t.Errorf("arg(id) = %q, %v; want %q, true", v, ok, "family1")
	}
}

// TestEncodeCallbackData_RoundTripsUnderRouterContract is the property that
// keeps the format framework-compatible: whatever the encoder emits, the
// router's own two steps (url.Parse, then dispatch on URL.Path) must recover
// the command in URL.Path — never in Scheme, Opaque or Host — and recover every
// argument verbatim.
func TestEncodeCallbackData_RoundTripsUnderRouterContract(t *testing.T) {
	tests := []struct {
		name    string
		command string
		args    url.Values
	}{
		{name: "no args", command: "space", args: nil},
		{name: "one arg", command: "space", args: url.Values{"id": {"family1"}}},
		{name: "two args", command: "space", args: url.Values{"id": {"family1"}, "tab": {"members"}}},
		// Values are user/space data, not constants: a space ID carrying URL
		// metacharacters must not leak out of the query string.
		{name: "value with separators", command: "space", args: url.Values{"id": {"a&b=c?d#e"}}},
		{name: "value with slash", command: "space", args: url.Values{"id": {"a/b"}}},
		{name: "value with space", command: "space", args: url.Values{"id": {"a b"}}},
		{name: "value with percent", command: "space", args: url.Values{"id": {"100%"}}},
		{name: "value with plus", command: "space", args: url.Values{"id": {"a+b"}}},
		{name: "value that looks like a scheme", command: "space", args: url.Values{"id": {"http://x/y"}}},
		{name: "empty value", command: "space", args: url.Values{"id": {""}}},
		{name: "unicode value", command: "space", args: url.Values{"id": {"семья"}}},
		// These commands are not shapes we expect to register, but they are
		// safe and it is worth pinning why: url.URL.String() escapes "?" and
		// "#" inside a path ("spa%3Fce", "spac%23e"), so neither can open a
		// query or a fragment, and the re-parse hands the command back intact.
		// A leading "/" survives verbatim as Path. Only ":" and a leading "//"
		// actually break — see the rejection test below.
		{name: "command with question mark", command: "spa?ce", args: url.Values{"id": {"family1"}}},
		{name: "command with hash", command: "spac#e", args: url.Values{"id": {"family1"}}},
		{name: "command with leading slash", command: "/space", args: url.Values{"id": {"family1"}}},
		{name: "command with inner slash", command: "space/members", args: url.Values{"id": {"family1"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := encodeCallbackData(tt.command, tt.args)
			if err != nil {
				t.Fatalf("encodeCallbackData(%q, %v) = %v, want no error", tt.command, tt.args, err)
			}

			// The router's contract, applied verbatim.
			u, err := url.Parse(data)
			if err != nil {
				t.Fatalf("url.Parse(%q) = %v, want no error", data, err)
			}
			if u.Path != tt.command {
				t.Errorf("url.Parse(%q).Path = %q, want %q", data, u.Path, tt.command)
			}
			if u.Scheme != "" || u.Opaque != "" || u.Host != "" {
				t.Errorf("url.Parse(%q) put the command outside Path: scheme=%q opaque=%q host=%q",
					data, u.Scheme, u.Opaque, u.Host)
			}
			if u.Fragment != "" {
				t.Errorf("url.Parse(%q).Fragment = %q, want empty (a fragment would swallow args)", data, u.Fragment)
			}
			for name, want := range tt.args {
				if got := u.Query().Get(name); got != want[0] {
					t.Errorf("url.Parse(%q).Query()[%s] = %q, want %q", data, name, got, want[0])
				}
			}

			// And our own parse helper agrees with the router.
			cd, err := parseCallbackData(data)
			if err != nil {
				t.Fatalf("parseCallbackData(%q) = %v, want no error", data, err)
			}
			if cd.command != tt.command {
				t.Errorf("parseCallbackData(%q).command = %q, want %q", data, cd.command, tt.command)
			}
			for name, want := range tt.args {
				if got, ok := cd.arg(name); !ok || got != want[0] {
					t.Errorf("arg(%s) = %q, %v; want %q, true", name, got, ok, want[0])
				}
			}
		})
	}
}

// TestEncodeCallbackData_RejectsCommandsThatWouldMisparse pins the subtlety
// that makes a naive encoder wrong. url.URL.String() does not guarantee a Path
// survives a re-parse as Path:
//
//	url.URL{Path: "//space"}.String()   == "//space?…"   → re-parses with Host="space", Path=""
//	url.URL{Path: "space:sub"}.String() == "./space:sub?…" → re-parses with Path="./space:sub"
//
// Either would dispatch on something other than the command we encoded, so the
// encoder must refuse rather than emit them. (url.PathEscape is no help: it
// does not escape ":".)
func TestEncodeCallbackData_RejectsCommandsThatWouldMisparse(t *testing.T) {
	tests := []struct {
		name    string
		command string
	}{
		{name: "empty", command: ""},
		{name: "leading double slash reads as authority", command: "//space"},
		{name: "colon reads as scheme", command: "space:sub"},
		{name: "colon in first segment", command: "a:b/c"},
		{name: "scheme and authority", command: "http://evil/space"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := encodeCallbackData(tt.command, url.Values{"id": {"family1"}})
			if err == nil {
				t.Fatalf("encodeCallbackData(%q, …) = %q, want an error", tt.command, data)
			}
		})
	}
}

// TestParseCallbackData_Unparseable covers the "does not parse" half of
// REQ: unrecognized-callback-data — data with no dispatchable command in
// URL.Path must come back as an error, never as a zero-value command that
// would silently no-op.
func TestParseCallbackData_Unparseable(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "empty", data: ""},
		{name: "invalid percent escape", data: "%zz?id=x"},
		{name: "control character", data: "sp\x7face?id=x"},
		{name: "no command, only args", data: "?id=family1"},
		{name: "command lands in host", data: "//space?id=family1"},
		{name: "command lands in scheme and opaque", data: "space:sub?id=family1"},
		{name: "absolute url", data: "http://evil/space?id=family1"},
		// url.Parse itself accepts this — it does not decode the query — and
		// url.URL.Query() would silently drop the undecodable argument, which
		// would reach a dispatcher disguised as a *missing* one. Parsing the
		// query strictly keeps "unhandleable" distinct from "omitted".
		{name: "undecodable query", data: "space?id=%zz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cd, err := parseCallbackData(tt.data)
			if err == nil {
				t.Fatalf("parseCallbackData(%q) = %+v, want an error", tt.data, cd)
			}
		})
	}
}

// TestParseCallbackData_UnknownPathIsNotAParseError draws the line Task 5
// depends on: a well-formed reference to a command nobody registered parses
// cleanly. Deciding it is unknown is the dispatcher's business, not the
// parser's — the two failures get different user-facing answers.
func TestParseCallbackData_UnknownPathIsNotAParseError(t *testing.T) {
	cd, err := parseCallbackData("nosuchcommand?id=family1")
	if err != nil {
		t.Fatalf("parseCallbackData = %v, want no error: an unknown command is not a parse failure", err)
	}
	if cd.command != "nosuchcommand" {
		t.Errorf("command = %q, want %q", cd.command, "nosuchcommand")
	}
}

// TestParseCallbackData_CommandWithoutArgs parses: a command may legitimately
// take no arguments, and that is not a failure either.
func TestParseCallbackData_CommandWithoutArgs(t *testing.T) {
	cd, err := parseCallbackData("space")
	if err != nil {
		t.Fatalf("parseCallbackData(%q) = %v, want no error", "space", err)
	}
	if cd.command != "space" {
		t.Errorf("command = %q, want %q", cd.command, "space")
	}
	if v, ok := cd.arg("id"); ok {
		t.Errorf("arg(id) = %q, true; want missing", v)
	}
}

// TestCallbackData_ArgDistinguishesMissingFromEmpty is the other seam Task 5
// needs: "omits a required argument" must be tellable from "passed it empty",
// which url.Values.Get alone cannot do — it returns "" for both.
func TestCallbackData_ArgDistinguishesMissingFromEmpty(t *testing.T) {
	cd, err := parseCallbackData("space?id=")
	if err != nil {
		t.Fatalf("parseCallbackData = %v, want no error", err)
	}
	if v, ok := cd.arg("id"); !ok || v != "" {
		t.Errorf("arg(id) = %q, %v; want %q, true (present but empty)", v, ok, "")
	}
	if v, ok := cd.arg("missing"); ok || v != "" {
		t.Errorf("arg(missing) = %q, %v; want %q, false", v, ok, "")
	}
}

// TestCallbackData_ArgOnZeroValueIsSafe: a zero callbackData has no args map,
// and reading one must report "missing" rather than panic.
func TestCallbackData_ArgOnZeroValueIsSafe(t *testing.T) {
	var cd callbackData
	if v, ok := cd.arg("id"); ok || v != "" {
		t.Errorf("arg on zero value = %q, %v; want %q, false", v, ok, "")
	}
}
