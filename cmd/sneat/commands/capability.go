package commands

import (
	"fmt"

	"github.com/sneat-co/sneat-cli/typespec/generated"
	"github.com/spf13/cobra"
)

type capabilityOperation = generated.CLIOperation

type capabilitySchema struct {
	Inputs []generated.InputSchema `json:"inputs"`
	Result string                  `json:"result"`
}

type capabilityHelp struct {
	CommandPath string   `json:"commandPath"`
	Aliases     []string `json:"aliases"`
	Formats     []string `json:"formats"`
}

// Capability exposes generated discovery, schema, and help metadata. It reads
// the checked TypeSpec artifact embedded in this CLI build; it does not infer
// capabilities from Cobra registrations or HTTP routes.
func Capability() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "capability",
		Short: "Discover generated CLI capabilities",
	}
	addFormatFlags(cmd)
	cmd.AddCommand(capabilityListCmd(), capabilitySchemaCmd(), capabilityHelpCmd())
	return cmd
}

func capabilityListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List generated CLI capabilities by stable operation ID",
		RunE: func(cmd *cobra.Command, _ []string) error {
			manifest, err := generated.CapabilityManifest()
			if err != nil {
				return err
			}
			return outputJSONDefault(cmd, manifest.Operations, nil, nil)
		},
	}
}

func capabilitySchemaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "schema <operation-id>",
		Short: "Inspect an operation's generated input and result schemas",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			operation, err := capabilityByID(args[0])
			if err != nil {
				return err
			}
			return outputJSONDefault(cmd, capabilitySchema{Inputs: operation.Inputs, Result: operation.Result}, nil, nil)
		},
	}
}

func capabilityHelpCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "help <operation-id>",
		Short: "Inspect generated command help metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			operation, err := capabilityByID(args[0])
			if err != nil {
				return err
			}
			return outputJSONDefault(cmd, capabilityHelp{
				CommandPath: operation.CommandPath,
				Aliases:     operation.Aliases,
				Formats:     operation.Formats,
			}, nil, nil)
		},
	}
}

func capabilityByID(operationID string) (generated.CLIOperation, error) {
	manifest, err := generated.CapabilityManifest()
	if err != nil {
		return generated.CLIOperation{}, err
	}
	for _, operation := range manifest.Operations {
		if operation.OperationID == operationID {
			return operation, nil
		}
	}
	return generated.CLIOperation{}, fmt.Errorf("unknown generated capability operation %q", operationID)
}
