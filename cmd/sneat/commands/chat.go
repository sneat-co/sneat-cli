package commands

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/sneat-co/sneat-cli/internal/aichat/aiconfig"
)

// Chat is the top-level `sneat chat` — launches the interactive chat session.
//
// The ai-* flags configure the aichat MVP pipeline (internal/aichat): which
// decision providers run (rules always; the Sneat AI Cloud/Jev provider
// unless --no-jev) and which LLM answers (Sneat AI Cloud, or a BYOK
// endpoint). They are read into internal/aichat/aiconfig.Overrides by
// aiChatOverridesFromCmd; RunChat's composition root resolves them the same
// flag>env>file>default way as every other sneat-cli config value.
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
	cmd.Flags().String("byok-api-key-env", "", "BYOK: env var holding the API key (default OPENAI_API_KEY / ANTHROPIC_API_KEY by protocol)")
	return cmd
}

// aiChatOverridesFromCmd reads the ai-* flags into aiconfig.Overrides.
func aiChatOverridesFromCmd(cmd *cobra.Command) aiconfig.Overrides {
	noJev, _ := cmd.Flags().GetBool("no-jev")
	llm, _ := cmd.Flags().GetString("ai-llm")
	endpoint, _ := cmd.Flags().GetString("byok-endpoint")
	model, _ := cmd.Flags().GetString("byok-model")
	protocol, _ := cmd.Flags().GetString("byok-protocol")
	apiKeyEnv, _ := cmd.Flags().GetString("byok-api-key-env")
	return aiconfig.Overrides{
		NoJev: noJev, NoJevSet: cmd.Flags().Changed("no-jev"),
		LLM: llm, BYOKEndpoint: endpoint, BYOKModel: model,
		BYOKProtocol: protocol, BYOKAPIKeyEnv: apiKeyEnv,
	}
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
	return env.RunChat(spaces, contacts, sess.UID, sess.Email)
}
