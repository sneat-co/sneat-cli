// Package generated exposes TypeSpec projection artifacts to the Go CLI.
package generated

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed cli-capabilities.json
var cliCapabilitiesJSON []byte

type CLIManifest struct {
	Version    string         `json:"version"`
	Operations []CLIOperation `json:"operations"`
}

type CLIOperation struct {
	OperationID string        `json:"operationID"`
	CommandPath string        `json:"commandPath"`
	Aliases     []string      `json:"aliases"`
	Inputs      []InputSchema `json:"inputs"`
	Result      string        `json:"result"`
	Formats     []string      `json:"formats"`
	Collection  string        `json:"collection"`
	Pagination  string        `json:"pagination"`
	Ordering    string        `json:"ordering"`
	Mutation    string        `json:"mutation"`
	Idempotency string        `json:"idempotency"`
	HTTP        *HTTPMapping  `json:"http,omitempty"`
}

type InputSchema struct {
	Name     string `json:"name"`
	Required bool   `json:"required"`
	Schema   string `json:"schema"`
}

type HTTPMapping struct {
	Method      string `json:"method"`
	OperationID string `json:"operationID"`
	Path        string `json:"path"`
}

func CapabilityManifest() (CLIManifest, error) {
	var manifest CLIManifest
	if err := json.Unmarshal(cliCapabilitiesJSON, &manifest); err != nil {
		return CLIManifest{}, fmt.Errorf("decode generated CLI capability manifest: %w", err)
	}
	if manifest.Version == "" {
		return CLIManifest{}, fmt.Errorf("generated CLI capability manifest has no version")
	}
	return manifest, nil
}
