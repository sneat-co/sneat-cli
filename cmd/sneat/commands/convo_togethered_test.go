package commands

import (
	"strings"
	"testing"
)

func TestConvoReplay_Togd_RecordIntent(t *testing.T) {
	buf, exec := buildConvoCmd(t)
	if err := exec("convo", "replay", "testdata/togethered_record_intent.txt", "--scope", "togethered"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "togethered.record_intent") {
		t.Errorf("output should contain togethered.record_intent:\n%s", out)
	}
}

func TestConvoReplay_Togd_ChangeIntent(t *testing.T) {
	buf, exec := buildConvoCmd(t)
	if err := exec("convo", "replay", "testdata/togethered_change_intent.txt", "--scope", "togethered"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "record_intent") {
		t.Errorf("output should contain record_intent:\n%s", out)
	}
	if !strings.Contains(out, "change_intent") {
		t.Errorf("output should contain change_intent:\n%s", out)
	}
}

func TestConvoReplay_Togd_CancelIntent(t *testing.T) {
	buf, exec := buildConvoCmd(t)
	if err := exec("convo", "replay", "testdata/togethered_cancel_intent.txt", "--scope", "togethered"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	// cancel_intent has Confirm=true so replay stages pending; "yes" resolves it.
	if !strings.Contains(out, "togethered.cancel_intent") {
		t.Errorf("output should contain togethered.cancel_intent:\n%s", out)
	}
}

func TestConvoReplay_Togd_Follow(t *testing.T) {
	buf, exec := buildConvoCmd(t)
	if err := exec("convo", "replay", "testdata/togethered_follow.txt", "--scope", "togethered"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "togethered.follow") {
		t.Errorf("output should contain togethered.follow:\n%s", out)
	}
	if !strings.Contains(out, "togethered.unfollow") {
		t.Errorf("output should contain togethered.unfollow:\n%s", out)
	}
}

func TestConvoReplay_Togd_QuerySpotDay(t *testing.T) {
	buf, exec := buildConvoCmd(t)
	if err := exec("convo", "replay", "testdata/togethered_query_spot_day.txt", "--scope", "togethered"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "togethered.query_spot_day") {
		t.Errorf("output should contain togethered.query_spot_day:\n%s", out)
	}
}
