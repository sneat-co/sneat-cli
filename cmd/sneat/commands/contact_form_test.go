package commands

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"charm.land/huh/v2"
)

func TestFirstOf(t *testing.T) {
	if got := firstOf(nil); got != "" {
		t.Fatalf("firstOf(nil) = %q, want empty", got)
	}
	if got := firstOf([]string{}); got != "" {
		t.Fatalf("firstOf([]) = %q, want empty", got)
	}
	if got := firstOf([]string{"a@b.c", "second@b.c"}); got != "a@b.c" {
		t.Fatalf("firstOf = %q, want a@b.c", got)
	}
}

func TestNonEmptySlice(t *testing.T) {
	if got := nonEmptySlice(""); got != nil {
		t.Fatalf("nonEmptySlice(\"\") = %v, want nil", got)
	}
	if got := nonEmptySlice("v"); len(got) != 1 || got[0] != "v" {
		t.Fatalf("nonEmptySlice(v) = %v, want [v]", got)
	}
}

// TestRunContactForm_PropagatesFormError exercises the error branch of
// RunContactForm via the runForm seam, without needing a real TTY.
func TestRunContactForm_PropagatesFormError(t *testing.T) {
	orig := runForm
	wantErr := errors.New("boom")
	runForm = func(*huh.Form) error { return wantErr }
	t.Cleanup(func() { runForm = orig })

	in := &contactInput{}
	err := RunContactForm(in)
	if !errors.Is(err, wantErr) {
		t.Fatalf("RunContactForm error = %v, want %v", err, wantErr)
	}
	// Fields collected after a failed form run must not be overwritten.
	if in.Emails != nil || in.Phones != nil || in.Roles != nil {
		t.Fatalf("fields set despite form error: %+v", in)
	}
}

// TestRunContactForm_AccessibleRun drives the real huh form end-to-end in
// accessible (line-based) mode, which huh selects automatically when
// TERM=dumb — no real terminal needed. This exercises RunContactForm's
// defaulting and field-collection statements against the genuine form.Run().
//
// huh creates a fresh bufio.Scanner per field, so answers are written one at
// a time, each only after that field's prompt appears on stdout; writing them
// all at once would let the first scanner swallow every line.
func TestRunContactForm_AccessibleRun(t *testing.T) {
	t.Setenv("TERM", "dumb")

	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	origStdin, origStdout := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = inR, outW
	t.Cleanup(func() {
		os.Stdin, os.Stdout = origStdin, origStdout
		_ = inR.Close()
		_ = outW.Close()
		_ = outR.Close()
	})
	// Watchdog: if a prompt never appears, EOF on stdin ends the form
	// (every field then keeps its default) instead of hanging the test.
	watchdog := time.AfterFunc(5*time.Second, func() { _ = inW.Close() })
	t.Cleanup(func() { watchdog.Stop() })

	// Prompt marker -> answer, in form order.
	steps := []struct{ marker, answer string }{
		{"Full name", "Alice Example"},
		{"Enter a number", "2"}, // Gender: 2 = male
		{"Enter a number", "4"}, // Age group: 4 = senior
		{"Email", "alice@example.test"},
		{"Phone", "+1234567890"},
		{"Role", "member"},
	}
	go func() {
		var seen strings.Builder
		offset, step := 0, 0
		buf := make([]byte, 4096)
		for {
			n, readErr := outR.Read(buf)
			seen.Write(buf[:n])
			for step < len(steps) {
				i := strings.Index(seen.String()[offset:], steps[step].marker)
				if i < 0 {
					break
				}
				offset += i + len(steps[step].marker)
				_, _ = io.WriteString(inW, steps[step].answer+"\n")
				step++
			}
			if readErr != nil {
				return
			}
		}
	}()

	in := &contactInput{}
	if err := RunContactForm(in); err != nil {
		t.Fatalf("RunContactForm: %v", err)
	}

	if in.Type != "person" {
		t.Fatalf("Type = %q, want person", in.Type)
	}
	if in.Name != "Alice Example" {
		t.Fatalf("Name = %q", in.Name)
	}
	if in.Gender != "male" {
		t.Fatalf("Gender = %q, want male", in.Gender)
	}
	if in.AgeGroup != "senior" {
		t.Fatalf("AgeGroup = %q, want senior", in.AgeGroup)
	}
	if len(in.Emails) != 1 || in.Emails[0] != "alice@example.test" {
		t.Fatalf("Emails = %v", in.Emails)
	}
	if len(in.Phones) != 1 || in.Phones[0] != "+1234567890" {
		t.Fatalf("Phones = %v", in.Phones)
	}
	if len(in.Roles) != 1 || in.Roles[0] != "member" {
		t.Fatalf("Roles = %v", in.Roles)
	}
}
