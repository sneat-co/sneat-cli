package chat

import (
	"fmt"
	"net/url"
)

// Callback data is URL-shaped — `<command>?<arg>=<value>` — because that is
// what bots-fw's router understands (REQ: callback-data-url). The router parses
// callback data with url.Parse and dispatches on the result's Path:
//
//	queryURL, err := url.Parse(query)
//	command := commands[botsfw.CommandCode(queryURL.Path)]
//
// (bots-fw/botswebhook/router.go). So the contract every helper here upholds is
// narrow and mechanical: the command must be recoverable from URL.Path by
// url.Parse alone, and the arguments from URL.Query(). Keeping to it is what
// lets a server-backed processor replace the in-process one without the
// renderer, or the buttons it renders, changing.

// callbackData is the parsed form of an inline button's callback data: the
// command to dispatch, and the arguments it was given.
//
// It separates the three outcomes a dispatcher must tell apart. Data that is
// not URL-shaped fails parseCallbackData with an error; a command that parses
// but names nothing registered is a non-error this type reports as command; and
// a required argument that was never passed is what arg reports as missing.
// Conflating those would cost the caller REQ: unrecognized-callback-data, which
// answers each of them rather than no-oping.
type callbackData struct {
	// command is the URL path: the code a dispatcher routes on.
	command string

	// args is the parsed query string.
	args url.Values
}

// arg returns the named argument and whether it was present.
//
// The bool is the point: url.Values.Get returns "" both for an argument that
// was omitted and for one passed empty, and a dispatcher owing the user
// "omits a required argument" cannot tell those apart from the value alone.
func (d callbackData) arg(name string) (string, bool) {
	vs, ok := d.args[name]
	if !ok || len(vs) == 0 {
		return "", false
	}
	return vs[0], true
}

// encodeCallbackData builds the callback data for a button: the command as the
// URL path, args as the query string. Passing nil args yields a bare command.
//
// It returns an error rather than data that would dispatch as something other
// than command. url.URL.String() does not promise a Path survives a re-parse as
// Path, and two shapes really do break — a command containing ":" is emitted
// with a defensive "./" prefix ("space:sub" -> "./space:sub?…"), and one
// starting with "//" is re-parsed as an authority ("//space?…" lands "space" in
// Host with an empty Path). Both would silently dispatch on the wrong code, so
// encoding verifies the round-trip under the router's own contract and refuses
// what does not hold. Escaping alone would not save us: url.PathEscape does not
// escape ":".
//
// Argument values need no such care — they are space IDs and other data, not
// constants — because url.Values.Encode escapes them into the query string,
// where "?", "#", "/" and "&" cannot break back out.
func encodeCallbackData(command string, args url.Values) (string, error) {
	u := url.URL{Path: command, RawQuery: args.Encode()}
	data := u.String()

	// Verify against the contract itself rather than against a hand-written
	// rule about which commands are safe: parse the result exactly as the
	// router would, and require the command back where the router looks.
	parsed, err := parseCallbackData(data)
	if err != nil {
		return "", fmt.Errorf("command %q cannot be encoded as callback data: %w", command, err)
	}
	if parsed.command != command {
		return "", fmt.Errorf(
			"command %q cannot be encoded as callback data: %q parses back as command %q",
			command, data, parsed.command)
	}
	return data, nil
}

// parseCallbackData parses callback data into a command and its arguments,
// applying the same url.Parse contract as bots-fw's router.
//
// An error means the data carries no command to dispatch — it is not a URL, or
// the command is not where the router reads it. Naming a command that nobody
// registered is not an error here: it parses, and answering it belongs to the
// dispatcher, which alone knows what is registered.
func parseCallbackData(data string) (callbackData, error) {
	u, err := url.Parse(data)
	if err != nil {
		return callbackData{}, fmt.Errorf("callback data %q is not a URL: %w", data, err)
	}
	// The router reads the command from Path only. Anything that steered it
	// elsewhere — a scheme, an opaque part, an authority — leaves Path holding
	// something other than the command, so refuse instead of dispatching on it.
	if u.Scheme != "" || u.Opaque != "" || u.Host != "" {
		return callbackData{}, fmt.Errorf(
			"callback data %q is not a bare <command>?<arg>=<value> reference", data)
	}
	if u.Path == "" {
		return callbackData{}, fmt.Errorf("callback data %q names no command", data)
	}
	// Parse the query strictly. url.URL.Query() drops what it cannot decode,
	// which would turn a mangled argument into an absent one and cost the
	// caller the difference between "unhandleable" and "missing argument".
	args, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return callbackData{}, fmt.Errorf("callback data %q has an unreadable query: %w", data, err)
	}
	return callbackData{command: u.Path, args: args}, nil
}
