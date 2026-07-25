package commands

import (
	"strings"
	"testing"
)

// `convo catalogs` answers "what is this runtime composed of" from the registry
// rather than from composition source, so it must name the extensions that are
// actually registered — not a list maintained by hand alongside them.
func TestConvoCatalogs_ListsRegisteredExtensions(t *testing.T) {
	buf, exec := buildConvoCmd(t)
	if err := exec("convo", "catalogs"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	for _, catalog := range []string{"listus", "trackus", "calendarius", "contactus"} {
		if !strings.Contains(out, catalog) {
			t.Errorf("catalog %q missing from output:\n%s", catalog, out)
		}
	}
	// Trigger vocabulary is the whole point of the listing.
	if !strings.Contains(out, "push-ups") {
		t.Errorf("declared triggers missing from output:\n%s", out)
	}
}

// A catalog declaring no triggers must SAY so. Left blank it reads as a
// rendering bug rather than as "this one can never narrow".
func TestConvoCatalogs_SaysWhenACatalogHasNoTriggers(t *testing.T) {
	buf, exec := buildConvoCmd(t)
	if err := exec("convo", "catalogs"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(buf.String(), "never narrows") {
		t.Errorf("a trigger-less catalog must be labelled, not left blank:\n%s", buf.String())
	}
}

func TestConvoCatalogs_ScopeNarrowsTheListing(t *testing.T) {
	buf, exec := buildConvoCmd(t)
	if err := exec("convo", "catalogs", "--scope", "trackus"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "trackus") {
		t.Errorf("scoped catalog missing:\n%s", out)
	}
	if strings.Contains(out, "listus") {
		t.Errorf("--scope trackus must not list listus:\n%s", out)
	}
}

// `convo route` explains a routing decision without calling a model. A phrase
// only one extension claims must be reported as narrowing to it.
func TestConvoRoute_ReportsTheClaimingCatalog(t *testing.T) {
	buf, exec := buildConvoCmd(t)
	if err := exec("convo", "route", "20 push-ups"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "trackus") {
		t.Errorf("route did not attribute %q to trackus:\n%s", "20 push-ups", out)
	}
	if !strings.Contains(out, "narrows") {
		t.Errorf("a single claiming catalog must be reported as narrowing:\n%s", out)
	}
}

// A phrase no extension claims must be reported as failing OPEN — the prefilter
// never drops a turn it cannot classify, and the report has to say so or it
// looks like the message was rejected.
func TestConvoRoute_UnclaimedTextFailsOpen(t *testing.T) {
	buf, exec := buildConvoCmd(t)
	if err := exec("convo", "route", "what is the meaning of life"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "FAILS OPEN") {
		t.Errorf("unclaimed text must be reported as failing open:\n%s", out)
	}
	if !strings.Contains(out, "normalized") {
		t.Errorf("route must show the normalized form it actually matched on:\n%s", out)
	}
}

// Narrowing to one catalog means the runtime skips the prefilter entirely, so
// claiming a trigger "narrowed" the scope would describe a step that never ran.
func TestConvoRoute_SingleCatalogScopeReportsNoPrefilter(t *testing.T) {
	buf, exec := buildConvoCmd(t)
	if err := exec("convo", "route", "20 push-ups", "--scope", "trackus"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "prefilter is skipped") {
		t.Errorf("with one catalog in scope the prefilter does not run, and the report must say so:\n%s", out)
	}
	if strings.Contains(out, "narrows to it") {
		t.Errorf("must not claim narrowing when the prefilter is skipped:\n%s", out)
	}
}
