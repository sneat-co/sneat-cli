package commands

import (
	"testing"

	"github.com/strongo/aichat/ai/aiconfig"
)

// TestAiChatConfigFromCmd_NoJevFlag covers scenario 10 (--no-jev): verifying
// the flag actually reaches aiconfig.Config.Decision.Provider is what makes
// it meaningful, rather than just existing on the command.
func TestAiChatConfigFromCmd_NoJevFlag(t *testing.T) {
	env := chatEnv(true, &fakeStore{}, new(string), new(int))
	cmd := Chat(env)
	if err := cmd.Flags().Parse([]string{"--no-jev", "--ai-llm", "byok", "--byok-endpoint", "https://example.test/v1", "--byok-protocol", "anthropic"}); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	cfg := aiChatConfigFromCmd(cmd, aiconfig.Config{})
	if cfg.Decision.Provider != "disabled" {
		t.Fatalf("Decision.Provider = %q, want disabled", cfg.Decision.Provider)
	}
	if !noJevFromCmd(cmd) {
		t.Fatal("noJevFromCmd = false, want true")
	}
	if cfg.LLM.Provider != "byok" || cfg.BYOK.Endpoint != "https://example.test/v1" || cfg.BYOK.Protocol != "anthropic" {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestAiChatConfigFromCmd_UnsetFlagsLeaveBaseUntouched(t *testing.T) {
	env := chatEnv(true, &fakeStore{}, new(string), new(int))
	cmd := Chat(env)
	base := aiconfig.Config{}
	base.LLM.Provider = "cloud"
	base.Decision.Provider = "auto"
	cfg := aiChatConfigFromCmd(cmd, base)
	if cfg.LLM.Provider != "cloud" || cfg.Decision.Provider != "auto" {
		t.Fatalf("cfg = %+v, want base's values unchanged when no flags are passed", cfg)
	}
	if noJevFromCmd(cmd) {
		t.Fatal("noJevFromCmd = true, want false when --no-jev was not passed")
	}
}
