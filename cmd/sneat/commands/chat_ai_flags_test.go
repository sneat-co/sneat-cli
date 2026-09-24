package commands

import "testing"

// TestAiChatOverridesFromCmd_NoJevFlag covers scenario 10 (--no-jev):
// verifying the flag actually reaches aiconfig.Overrides is what makes it
// meaningful, rather than just existing on the command.
func TestAiChatOverridesFromCmd_NoJevFlag(t *testing.T) {
	env := chatEnv(true, &fakeStore{}, new(string), new(int))
	cmd := Chat(env)
	if err := cmd.Flags().Parse([]string{"--no-jev", "--ai-llm", "byok", "--byok-endpoint", "https://example.test/v1", "--byok-protocol", "anthropic"}); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	o := aiChatOverridesFromCmd(cmd)
	if !o.NoJev || !o.NoJevSet {
		t.Fatal("expected --no-jev to set NoJev/NoJevSet")
	}
	if o.LLM != "byok" || o.BYOKEndpoint != "https://example.test/v1" || o.BYOKProtocol != "anthropic" {
		t.Fatalf("overrides = %+v", o)
	}
}

func TestAiChatOverridesFromCmd_DefaultsUnset(t *testing.T) {
	env := chatEnv(true, &fakeStore{}, new(string), new(int))
	cmd := Chat(env)
	o := aiChatOverridesFromCmd(cmd)
	if o.NoJevSet {
		t.Fatal("NoJevSet must be false when --no-jev was not passed")
	}
}
