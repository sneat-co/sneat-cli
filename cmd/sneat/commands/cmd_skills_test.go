package commands

import (
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/strongo/cli-helpers/selfupdate"
	"github.com/strongo/cli-helpers/skillsync"
	skillscobracmd "github.com/strongo/cli-helpers/skillsync/cobracmd"
)

func TestSkillsCommand_Shape(t *testing.T) {
	t.Parallel()

	cmd := SkillsCommand("0.14.0")
	if cmd.Name() != "skills" {
		t.Errorf("Name() = %q, want %q", cmd.Name(), "skills")
	}
	if cmd.Short == "" {
		t.Error("Short is empty, want a description")
	}

	syncCmd, _, err := cmd.Find([]string{"sync"})
	if err != nil || syncCmd == nil || syncCmd.Name() != "sync" {
		t.Fatalf("missing sync subcommand: %v", err)
	}

	for _, name := range []string{"harness", "dir", "dry-run", "newer-compatible", "format"} {
		if syncCmd.Flags().Lookup(name) == nil {
			t.Errorf("missing --%s flag on sync command", name)
		}
	}
}

func TestSkillsErrors_Failure(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want int
	}{
		{"usage error", &skillscobracmd.UsageError{Err: errors.New("invalid flag")}, 2},
		{"unknown target", &selfupdate.Failure{Kind: selfupdate.KindUnknownTarget, Err: errors.New("unknown target")}, 2},
		{"plain error", errors.New("plain error"), 1},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := (skillsErrors{}).Failure(c.err)
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

func TestSkillsErrors_Conflict(t *testing.T) {
	t.Parallel()

	report := skillsync.Report{
		Dir: "/path/to/skills",
	}
	err := (skillsErrors{}).Conflict(report)
	var exitErr *ExitCodeError
	if !errors.As(err, &exitErr) {
		t.Fatalf("Conflict() = %v (%T), want an *ExitCodeError", err, err)
	}
	if exitErr.Code != 1 {
		t.Errorf("Conflict() code = %d, want 1", exitErr.Code)
	}
	if exitErr.Err == nil || exitErr.Err.Error() == "" {
		t.Error("Conflict() error message is empty")
	}
}

func TestSneatSkillsSyncConfig(t *testing.T) {
	t.Parallel()

	cfg, err := sneatSkillsSyncConfig("0.14.0")
	if err != nil {
		t.Fatalf("sneatSkillsSyncConfig() error = %v", err)
	}
	if cfg.CLI.Publisher != "sneat" || cfg.CLI.Name != "sneat" {
		t.Errorf("CLI = %+v, want sneat/sneat", cfg.CLI)
	}
	if len(cfg.Bundles) != 1 {
		t.Fatalf("Bundles count = %d, want 1", len(cfg.Bundles))
	}
	bundle := cfg.Bundles[0]
	if bundle.Plugin.Publisher != "sneat" || bundle.Plugin.Name != "sneat" {
		t.Errorf("Plugin = %+v, want sneat/sneat", bundle.Plugin)
	}
	if bundle.Source.Version != "0.14.0" {
		t.Errorf("Source.Version = %q, want 0.14.0", bundle.Source.Version)
	}
	if bundle.Source.Path != "ai/skills" {
		t.Errorf("Source.Path = %q, want ai/skills", bundle.Source.Path)
	}
}

func TestSneatSkillsSyncConfig_DevVersion(t *testing.T) {
	t.Parallel()

	cfg, err := sneatSkillsSyncConfig("dev")
	if err != nil {
		t.Fatalf("sneatSkillsSyncConfig(dev) error = %v", err)
	}
	if len(cfg.Bundles) != 1 {
		t.Fatalf("Bundles count = %d, want 1", len(cfg.Bundles))
	}
	if cfg.Bundles[0].Source.Version != "0.0.0" {
		t.Errorf("Source.Version = %q, want 0.0.0 for dev version", cfg.Bundles[0].Source.Version)
	}
}

type errFS struct {
	err error
}

func (e errFS) Open(name string) (fs.File, error) {
	return nil, e.err
}

type badDigestFS struct{}

func (badDigestFS) Open(name string) (fs.File, error) {
	if name == "." {
		return badDir{}, nil
	}
	return nil, errors.New("open error")
}

type badDir struct{}

func (badDir) Stat() (fs.FileInfo, error) { return nil, errors.New("stat error") }
func (badDir) Read([]byte) (int, error)  { return 0, errors.New("read error") }
func (badDir) Close() error              { return nil }

func TestSkillsCommand_ConfigError(t *testing.T) {
	prev := sneatSkillsFS
	sneatSkillsFS = errFS{err: errors.New("injected fs error")}
	t.Cleanup(func() { sneatSkillsFS = prev })

	cmd := SkillsCommand("0.15.0")
	if cmd.RunE == nil {
		t.Fatal("expected cmd.RunE to be set when config has error")
	}
	err := cmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected RunE to return an error, got nil")
	}
	var exitErr *ExitCodeError
	if !errors.As(err, &exitErr) {
		t.Fatalf("expected ExitCodeError, got %T: %v", err, err)
	}
	if exitErr.Code != 1 {
		t.Errorf("expected exit code 1, got %d", exitErr.Code)
	}
}

type errSubFS struct{}

func (errSubFS) Open(name string) (fs.File, error) { return nil, errors.New("open error") }
func (errSubFS) Sub(dir string) (fs.FS, error)     { return nil, errors.New("sub error") }

func TestSneatSkillsSyncConfig_SubError(t *testing.T) {
	prev := sneatSkillsFS
	sneatSkillsFS = errSubFS{}
	t.Cleanup(func() { sneatSkillsFS = prev })

	_, err := sneatSkillsSyncConfig("0.15.0")
	if err == nil {
		t.Fatal("expected error from sneatSkillsSyncConfig with errSubFS, got nil")
	}
}

func TestSneatSkillsSyncConfig_DigestError(t *testing.T) {
	prev := sneatSkillsFS
	sneatSkillsFS = fstest.MapFS{
		"skills": &fstest.MapFile{Mode: fs.ModeDir},
		"skills/test": &fstest.MapFile{Data: []byte("invalid"), Mode: 0},
	}
	t.Cleanup(func() { sneatSkillsFS = prev })

	// Corrupt fs.Sub
	sneatSkillsFS = fstest.MapFS{
		"skills": &fstest.MapFile{
			Mode: fs.ModeDir,
		},
	}
	_, err := sneatSkillsSyncConfig("0.15.0")
	if err == nil {
		t.Fatal("expected error from sneatSkillsSyncConfig when skills directory has no valid bundle digest, got nil")
	}
}

