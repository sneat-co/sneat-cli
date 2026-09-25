package commands

import (
	"errors"
	"testing"
)

func TestExitCodeError_ErrorAndUnwrap(t *testing.T) {
	inner := errors.New("validation still needs input")
	e := &ExitCodeError{Code: 2, Err: inner}

	if got := e.Error(); got != "validation still needs input" {
		t.Fatalf("Error() = %q, want %q", got, inner.Error())
	}
	if !errors.Is(e, inner) {
		t.Fatalf("errors.Is(e, inner) = false, want true (Unwrap must expose inner)")
	}
	if unwrapped := errors.Unwrap(e); unwrapped != inner {
		t.Fatalf("errors.Unwrap(e) = %v, want %v", unwrapped, inner)
	}
}
