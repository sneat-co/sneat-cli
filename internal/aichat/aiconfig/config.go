// Package aiconfig resolves the sneat-chat MVP's AI configuration (brief
// §16: flag > env > default, plus an optional YAML file) and assembles the
// providers the pipeline needs from it.
//
// It intentionally does not depend on strongo/aichat's own ai/aiconfig
// package: that package was still an empty stub in the parallel aichat-ai
// lane's worktree as of this slice (see the final report), so this package
// builds providers directly from ai/decision/rules, ai/openaicompat,
// ai/anthropic, ai/cloud and ai/decision.Chain, which were already pushed
// and match the brief's pinned signatures exactly. Once ai/aiconfig lands,
// this file's Build should delegate to it and keep only the flag/env/YAML
// plumbing, which is Sneat-specific.
package aiconfig

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/strongo/aichat/ai"
	"github.com/strongo/aichat/ai/anthropic"
	"github.com/strongo/aichat/ai/cloud"
	"github.com/strongo/aichat/ai/decision"
	"github.com/strongo/aichat/ai/openaicompat"

	sneatrules "github.com/sneat-co/sneat-cli/internal/aichat/rules"
)

// LLM provider selection.
const (
	LLMCloud = "cloud"
	LLMBYOK  = "byok"
)

// BYOK protocol selection.
const (
	ProtocolOpenAICompatible = "openai-compatible"
	ProtocolAnthropic        = "anthropic"
)

// Config is the resolved AI configuration for one `sneat chat` run.
type Config struct {
	// NoJev disables the cloud decision provider (`--no-jev`). The rules
	// provider always runs; only the cloud leg of the chain is affected.
	NoJev bool
	// LLM selects the main-LLM provider: LLMCloud or LLMBYOK.
	LLM string
	// BYOK fields, meaningful when LLM == LLMBYOK.
	BYOKProtocol  string
	BYOKEndpoint  string
	BYOKModel     string
	BYOKAPIKeyEnv string
}

// FileConfig is the shape of <UserConfigDir>/sneat/ai.yaml (brief §16's
// illustrative ai.yaml, narrowed to what this MVP slice reads). Any field
// left unset here falls back to env then default, same as every other
// sneat-cli config source.
type FileConfig struct {
	AI struct {
		LLM struct {
			Provider string `yaml:"provider"`
		} `yaml:"llm"`
		Decision struct {
			Provider string `yaml:"provider"` // "auto" | "cloud" | "disabled"
		} `yaml:"decision"`
		BYOK struct {
			Protocol  string `yaml:"protocol"`
			Endpoint  string `yaml:"endpoint"`
			Model     string `yaml:"model"`
			APIKeyEnv string `yaml:"apiKeyEnv"`
		} `yaml:"byok"`
	} `yaml:"ai"`
}

// DefaultConfigPath returns <UserConfigDir>/sneat/ai.yaml, or "" when the
// user config directory cannot be determined.
func DefaultConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "sneat", "ai.yaml")
}

// Overrides are the flag-supplied values; zero value means "not set" for
// every field except NoJev, which needs its own presence flag since false is
// a valid explicit value.
type Overrides struct {
	NoJev         bool
	NoJevSet      bool
	LLM           string
	BYOKProtocol  string
	BYOKEndpoint  string
	BYOKModel     string
	BYOKAPIKeyEnv string
}

// Resolve applies flag > env > file > default for each field. file may be
// the zero FileConfig (e.g. when no ai.yaml exists).
func Resolve(o Overrides, file FileConfig, getenv func(string) string) Config {
	c := Config{
		LLM:          pick(o.LLM, file.AI.LLM.Provider, getenv("SNEAT_AI_LLM"), LLMCloud),
		BYOKProtocol: pick(o.BYOKProtocol, file.AI.BYOK.Protocol, getenv("SNEAT_AI_BYOK_PROTOCOL"), ProtocolOpenAICompatible),
		BYOKEndpoint: pick(o.BYOKEndpoint, file.AI.BYOK.Endpoint, getenv("SNEAT_AI_BYOK_ENDPOINT"), ""),
		BYOKModel:    pick(o.BYOKModel, file.AI.BYOK.Model, getenv("SNEAT_AI_BYOK_MODEL"), ""),
	}
	c.BYOKAPIKeyEnv = pick(o.BYOKAPIKeyEnv, file.AI.BYOK.APIKeyEnv, getenv("SNEAT_AI_BYOK_API_KEY_ENV"), defaultAPIKeyEnv(c.BYOKProtocol))
	switch {
	case o.NoJevSet:
		c.NoJev = o.NoJev
	case file.AI.Decision.Provider == "disabled":
		c.NoJev = true
	default:
		c.NoJev = false
	}
	return c
}

func pick(vals ...string) string {
	for _, v := range vals[:len(vals)-1] {
		if v != "" {
			return v
		}
	}
	return vals[len(vals)-1]
}

// defaultAPIKeyEnv picks OPENAI_API_KEY / ANTHROPIC_API_KEY by protocol, per
// brief §9.
func defaultAPIKeyEnv(protocol string) string {
	if protocol == ProtocolAnthropic {
		return "ANTHROPIC_API_KEY"
	}
	return "OPENAI_API_KEY"
}

// CloudDeps are what building the Sneat AI Cloud LLM/decision client needs:
// the api.sneat.cloud base URL, the product id ("sneat"), and the session's
// bearer token source (internal/tokensrc, reused as-is via this func type
// rather than redefined here) and HTTP client.
type CloudDeps struct {
	BaseURL string
	Product string
	Token   func(context.Context) (string, error)
	HTTP    *http.Client
}

// Providers is what Build hands the pipeline: the assembled decision chain
// (rules first, then the cloud decision provider unless NoJev) and the main
// LLM provider. LLM is nil when it could not be built -- BYOK with no
// endpoint configured, or Cloud with no CloudDeps -- and callers must treat
// that as "main LLM unavailable" (NeedsLLM turns become a plain "I don't
// know how to help with that yet"), never a panic.
type Providers struct {
	Chain decision.Chain
	LLM   ai.LLMProvider
}

// Build assembles the decision chain and the main LLM provider from cfg.
// cloudDeps is used only when cfg.LLM == LLMCloud or (cfg.LLM == LLMBYOK and
// !cfg.NoJev, since Jev/decision is independent of which LLM answers --
// brief §11: "a user may use Sneat Cloud/Jev for decisions while using their
// own BYOK LLM"); cloudDeps.Token == nil is treated as "cloud not
// configured", not an error, so a signed-out BYOK-only run still works.
func Build(cfg Config, cloudDeps CloudDeps) (Providers, error) {
	chainProviders := []decision.Provider{sneatrules.New()}

	var cloudClient *cloud.Client
	if cloudDeps.Token != nil && cloudDeps.BaseURL != "" {
		cloudClient = cloud.New(cloud.Config{
			BaseURL: cloudDeps.BaseURL, Product: cloudDeps.Product,
			Token: cloudDeps.Token, HTTPClient: cloudDeps.HTTP,
		})
	}
	if !cfg.NoJev && cloudClient != nil {
		chainProviders = append(chainProviders, cloudClient)
	}

	var llm ai.LLMProvider
	switch cfg.LLM {
	case LLMBYOK:
		if cfg.BYOKEndpoint != "" {
			switch cfg.BYOKProtocol {
			case ProtocolAnthropic:
				llm = anthropic.New(anthropic.Config{
					BaseURL: cfg.BYOKEndpoint, Model: cfg.BYOKModel,
					APIKey: os.Getenv(cfg.BYOKAPIKeyEnv), HTTPClient: cloudDeps.HTTP,
				})
			default:
				llm = openaicompat.New(openaicompat.Config{
					BaseURL: cfg.BYOKEndpoint, Model: cfg.BYOKModel,
					APIKey: os.Getenv(cfg.BYOKAPIKeyEnv), HTTPClient: cloudDeps.HTTP,
				})
			}
		}
	case LLMCloud:
		if cloudClient != nil {
			llm = cloudClient
		}
	default:
		return Providers{}, fmt.Errorf("aiconfig: unknown ai.llm provider %q", cfg.LLM)
	}

	return Providers{Chain: decision.Chain{Providers: chainProviders}, LLM: llm}, nil
}
