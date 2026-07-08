package config

import "testing"

func TestResolve_DefaultsWhenEmpty(t *testing.T) {
	c := Resolve(Overrides{}, func(string) string { return "" })
	if c.Project != DefaultProject {
		t.Fatalf("project = %q, want %q", c.Project, DefaultProject)
	}
	if c.APIBaseURL != DefaultAPIBaseURL {
		t.Fatalf("apiBaseURL = %q", c.APIBaseURL)
	}
	if c.AuthEmulatorHost != "" {
		t.Fatalf("authEmulatorHost = %q, want empty", c.AuthEmulatorHost)
	}
}

func TestResolve_FlagBeatsEnvBeatsDefault(t *testing.T) {
	env := map[string]string{"SNEAT_FIREBASE_PROJECT": "from-env"}
	getenv := func(k string) string { return env[k] }

	c := Resolve(Overrides{}, getenv)
	if c.Project != "from-env" {
		t.Fatalf("env not applied: %q", c.Project)
	}
	c = Resolve(Overrides{Project: "from-flag"}, getenv)
	if c.Project != "from-flag" {
		t.Fatalf("flag did not beat env: %q", c.Project)
	}
}

func TestResolve_EmulatorConvenienceSetsHosts(t *testing.T) {
	c := Resolve(Overrides{Emulator: true}, func(string) string { return "" })
	if c.AuthEmulatorHost != DefaultAuthEmulatorHost {
		t.Fatalf("authEmulatorHost = %q", c.AuthEmulatorHost)
	}
	if c.FirestoreEmulatorHost != DefaultFirestoreEmulatorHost {
		t.Fatalf("firestoreEmulatorHost = %q", c.FirestoreEmulatorHost)
	}
}
