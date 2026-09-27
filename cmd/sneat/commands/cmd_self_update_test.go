package commands

import (
	"errors"
	"testing"

	"github.com/strongo/cli-helpers/cliinstall"
	"github.com/strongo/cli-helpers/selfupdate"
	selfupdatecobracmd "github.com/strongo/cli-helpers/selfupdate/cobracmd"
)

func TestSelfUpdateCommand_Shape(t *testing.T) {
	t.Parallel()

	cmd := SelfUpdateCommand("0.14.0")
	if cmd.Name() != "self-update" {
		t.Errorf("Name() = %q, want %q", cmd.Name(), "self-update")
	}
	for _, name := range []string{"check", "yes", "dry-run", "version"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("missing --%s flag", name)
		}
	}
	if f := cmd.Flags().Lookup("yes"); f.Shorthand != "y" {
		t.Errorf("--yes shorthand = %q, want y", f.Shorthand)
	}
	if cmd.Short == "" {
		t.Error("Short is empty, want a description")
	}
}

func TestSelfUpdateErrors_Failure(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want int
	}{
		{"usage error", &selfupdatecobracmd.UsageError{Err: errors.New("invalid flag")}, 2},
		{"downgrade refusal", &selfupdate.Failure{Kind: selfupdate.KindDowngrade, Err: errors.New("downgrade")}, 2},
		{"unknown tag", &selfupdate.Failure{Kind: selfupdate.KindUnknownTag, Err: errors.New("unknown tag")}, 3},
		{"unsupported platform", &selfupdate.Failure{Kind: selfupdate.KindUnsupportedPlatform, Err: errors.New("unsupported platform")}, 3},
		{"release lookup failure", &selfupdate.Failure{Kind: selfupdate.KindReleaseLookup, Err: errors.New("lookup failed")}, 4},
		{"download failure", &selfupdate.Failure{Kind: selfupdate.KindDownload, Err: errors.New("download failed")}, 4},
		{"checksum failure", &selfupdate.Failure{Kind: selfupdate.KindChecksum, Err: errors.New("checksum mismatch")}, 1},
		{"plain error", errors.New("plain error"), 1},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := (selfUpdateErrors{}).Failure(c.err)
			var exitErr *ExitCodeError
			if !errors.As(got, &exitErr) {
				t.Fatalf("Failure(%v) = %v (%T), want an *ExitCodeError", c.err, got, got)
			}
			if exitErr.Code != c.want {
				t.Errorf("Failure(%v) code = %d, want %d", c.err, exitErr.Code, c.want)
			}
		})
	}
}

func TestSelfUpdateErrors_UpdateAvailable(t *testing.T) {
	t.Parallel()
	if err := (selfUpdateErrors{}).UpdateAvailable(selfupdate.CheckResult{}); err != nil {
		t.Errorf("UpdateAvailable() = %v, want nil", err)
	}
}

func TestSneatSelfUpdateConfig_PanicsWhenCatalogEntryMissing(t *testing.T) {
	catalogByID = func(id string) (cliinstall.Entry, bool) {
		return cliinstall.Entry{}, false
	}
	defer func() {
		catalogByID = cliinstall.ByID
		if r := recover(); r == nil {
			t.Fatal("expected panic when sneat catalog entry is missing")
		}
	}()
	sneatSelfUpdateConfig("0.14.0")
}
