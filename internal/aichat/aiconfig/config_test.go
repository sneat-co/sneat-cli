package aiconfig

import (
	"context"
	"testing"
)

func noEnv(string) string { return "" }

func TestResolve_Defaults(t *testing.T) {
	c := Resolve(Overrides{}, FileConfig{}, noEnv)
	if c.LLM != LLMCloud {
		t.Errorf("LLM = %q, want %q", c.LLM, LLMCloud)
	}
	if c.NoJev {
		t.Error("NoJev should default to false")
	}
	if c.BYOKProtocol != ProtocolOpenAICompatible {
		t.Errorf("BYOKProtocol = %q", c.BYOKProtocol)
	}
	if c.BYOKAPIKeyEnv != "OPENAI_API_KEY" {
		t.Errorf("BYOKAPIKeyEnv = %q", c.BYOKAPIKeyEnv)
	}
}

func TestResolve_NoJevFlag(t *testing.T) {
	c := Resolve(Overrides{NoJev: true, NoJevSet: true}, FileConfig{}, noEnv)
	if !c.NoJev {
		t.Error("--no-jev must disable Jev")
	}
}

func TestResolve_FileDecisionDisabled(t *testing.T) {
	var f FileConfig
	f.AI.Decision.Provider = "disabled"
	c := Resolve(Overrides{}, f, noEnv)
	if !c.NoJev {
		t.Error("decision.provider: disabled in the file must disable Jev")
	}
}

func TestResolve_FlagOverridesFileAndEnv(t *testing.T) {
	var f FileConfig
	f.AI.LLM.Provider = LLMCloud
	getenv := func(k string) string {
		if k == "SNEAT_AI_LLM" {
			return LLMBYOK
		}
		return ""
	}
	c := Resolve(Overrides{LLM: LLMBYOK}, f, getenv)
	if c.LLM != LLMBYOK {
		t.Errorf("LLM = %q, want flag value %q", c.LLM, LLMBYOK)
	}
}

func TestResolve_AnthropicDefaultAPIKeyEnv(t *testing.T) {
	c := Resolve(Overrides{BYOKProtocol: ProtocolAnthropic}, FileConfig{}, noEnv)
	if c.BYOKAPIKeyEnv != "ANTHROPIC_API_KEY" {
		t.Errorf("BYOKAPIKeyEnv = %q, want ANTHROPIC_API_KEY", c.BYOKAPIKeyEnv)
	}
}

func TestBuild_RulesOnlyChainWhenNoJev(t *testing.T) {
	cfg := Config{NoJev: true, LLM: LLMBYOK, BYOKEndpoint: "https://example.test/v1", BYOKProtocol: ProtocolOpenAICompatible}
	providers, err := Build(cfg, CloudDeps{
		BaseURL: "https://api.sneat.cloud/v0/", Product: "sneat",
		Token: func(context.Context) (string, error) { return "tok", nil },
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(providers.Chain.Providers) != 1 {
		t.Fatalf("chain providers = %d, want 1 (rules only) when NoJev", len(providers.Chain.Providers))
	}
	if providers.LLM == nil || providers.LLM.Name() != "openai-compatible" {
		t.Fatalf("LLM = %v, want the BYOK openai-compatible provider", providers.LLM)
	}
}

func TestBuild_AddsCloudDecisionUnlessNoJev(t *testing.T) {
	cfg := Config{LLM: LLMBYOK, BYOKEndpoint: "https://example.test/v1"}
	providers, err := Build(cfg, CloudDeps{
		BaseURL: "https://api.sneat.cloud/v0/", Product: "sneat",
		Token: func(context.Context) (string, error) { return "tok", nil },
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(providers.Chain.Providers) != 2 {
		t.Fatalf("chain providers = %d, want 2 (rules + cloud decision)", len(providers.Chain.Providers))
	}
}

func TestBuild_NoCloudTokenMeansNoCloudProvider(t *testing.T) {
	cfg := Config{LLM: LLMCloud}
	providers, err := Build(cfg, CloudDeps{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(providers.Chain.Providers) != 1 {
		t.Fatalf("chain providers = %d, want 1 (rules only, no signed-in cloud client)", len(providers.Chain.Providers))
	}
	if providers.LLM != nil {
		t.Fatal("LLM must be nil when cloud is requested but not configured")
	}
}

func TestBuild_UnknownLLMProviderIsAnError(t *testing.T) {
	_, err := Build(Config{LLM: "carrier-pigeon"}, CloudDeps{})
	if err == nil {
		t.Fatal("expected an error for an unknown ai.llm provider")
	}
}
