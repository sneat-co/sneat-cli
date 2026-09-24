package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/strongo/aichat/ai/aiconfig"
)

// aiConfigPath returns <UserConfigDir>/sneat/ai.yaml, or "" when the user
// config directory cannot be determined -- aiconfig.Load treats a missing
// file as "use defaults", never an error, so "" is a safe fallback too.
func aiConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "sneat", "ai.yaml")
}

// Chat is the top-level `sneat chat` — launches the interactive chat session.
//
// The ai-* flags configure the aichat MVP pipeline (internal/aichat): which
// decision providers run (the deterministic rules provider always; the
// Sneat AI Cloud/Jev decision provider unless --no-jev) and which LLM
// answers (Sneat AI Cloud, or a BYOK endpoint). aiChatOverridesFromCmd reads
// them into an aiconfig.Config that RunChat's composition root feeds to
// aiconfig.Build alongside aiconfig.Deps (EnvPrefix "SNEAT_", CloudBaseURL,
// CloudToken, ExtraDecision: the rules provider, DisableCloudDecision:
// --no-jev).
func Chat(env Env) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Chat with your spaces in an interactive terminal session",
		RunE:  func(cmd *cobra.Command, _ []string) error { return runChat(env, cmd) },
	}
	cmd.Flags().Bool("no-jev", false, "disable the Sneat AI Cloud decision provider (Jev); deterministic rules and the main LLM still work")
	cmd.Flags().String("ai-llm", "", "main LLM provider: cloud (default) or byok")
	cmd.Flags().String("byok-endpoint", "", "BYOK: the LLM API base URL")
	cmd.Flags().String("byok-model", "", "BYOK: the model id")
	cmd.Flags().String("byok-protocol", "", "BYOK: openai-compatible (default) or anthropic")
	cmd.Flags().String("byok-api-key-env", "", "BYOK: env var holding the API key")
	cmd.Flags().String("tz", "", "IANA timezone day/week windows and slot times resolve in (default: $SNEAT_TZ, else the system's local zone)")
	return cmd
}

// aiChatConfigFromCmd builds an aiconfig.Config from the ai-* flags layered
// onto base (typically aiconfig.Load's result). Flags not passed leave
// base's value (file/default) untouched; aiconfig.Build itself then layers
// SNEAT_-prefixed env vars on top via aiconfig.Deps.EnvPrefix.
func aiChatConfigFromCmd(cmd *cobra.Command, base aiconfig.Config) aiconfig.Config {
	cfg := base
	if v, _ := cmd.Flags().GetBool("no-jev"); v && cmd.Flags().Changed("no-jev") {
		cfg.Decision.Provider = "disabled"
	}
	if v, _ := cmd.Flags().GetString("ai-llm"); v != "" {
		cfg.LLM.Provider = v
	}
	if v, _ := cmd.Flags().GetString("byok-endpoint"); v != "" {
		cfg.BYOK.Endpoint = v
	}
	if v, _ := cmd.Flags().GetString("byok-model"); v != "" {
		cfg.BYOK.Model = v
	}
	if v, _ := cmd.Flags().GetString("byok-protocol"); v != "" {
		cfg.BYOK.Protocol = v
	}
	if v, _ := cmd.Flags().GetString("byok-api-key-env"); v != "" {
		cfg.BYOK.APIKeyEnv = v
	}
	return cfg
}

// tzFromCmd resolves the IANA zone the chat session's day/week windows and
// slot times work in (coordinator ruling TIMEZONES): --tz wins when passed;
// else SNEAT_TZ; else the system's own local zone name. time.Local.String()
// returns "Local" (not an IANA name) when the system zone could not be
// determined (e.g. no /etc/localtime, common in a minimal container) -- that
// is still a usable, honest value for the readers/decision Request to carry,
// never treated as an error here.
func tzFromCmd(cmd *cobra.Command, getenv func(string) string) string {
	if v, _ := cmd.Flags().GetString("tz"); v != "" {
		return v
	}
	if v := getenv("SNEAT_TZ"); v != "" {
		return v
	}
	return time.Local.String()
}

// noJevFromCmd reports whether --no-jev was passed, for
// aiconfig.Deps.DisableCloudDecision (kept separate from
// aiChatConfigFromCmd's Decision.Provider="disabled" so the flag's intent is
// unambiguous even if a product config file also sets decision.provider).
func noJevFromCmd(cmd *cobra.Command) bool {
	v, _ := cmd.Flags().GetBool("no-jev")
	return v && cmd.Flags().Changed("no-jev")
}

// runChat checks the startup preconditions — a terminal and a signed-in
// session — then resolves the spaces reader and delegates to env.RunChat.
// Every check runs before any terminal program exists, so the command stays
// testable without a TTY.
func runChat(env Env, cmd *cobra.Command) error {
	if env.IsTerminal != nil && !env.IsTerminal() {
		return fmt.Errorf("the interactive chat requires a terminal")
	}
	cfg := configFromCmd(cmd, env.Getenv)
	sess, err := env.Store.Load()
	if err != nil {
		return err
	}
	spaces, err := env.NewSpacesReader(cfg)
	if err != nil {
		return err
	}
	contacts, err := env.NewContactsReader(cfg)
	if err != nil {
		return err
	}
	aiBase, err := aiconfig.Load(aiConfigPath())
	if err != nil {
		return err
	}
	aiCfg := aiChatConfigFromCmd(cmd, aiBase)
	return env.RunChat(RunChatArgs{
		Spaces: spaces, Contacts: contacts, UID: sess.UID, Email: sess.Email,
		AIConfig: aiCfg, NoJev: noJevFromCmd(cmd), Cfg: cfg,
		CurrentSpace: sess.CurrentSpace,
		TZ:           tzFromCmd(cmd, env.Getenv),
	})
}
