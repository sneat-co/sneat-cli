package commands

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestCapabilityCommandsReadTheGeneratedManifest(t *testing.T) {
	list := Capability()
	var listOut bytes.Buffer
	list.SetOut(&listOut)
	list.SetArgs([]string{"list", "--json"})
	if err := list.Execute(); err != nil {
		t.Fatalf("capability list: %v", err)
	}
	var operations []capabilityOperation
	if err := json.Unmarshal(listOut.Bytes(), &operations); err != nil {
		t.Fatalf("decode list: %v\n%s", err, listOut.String())
	}
	if len(operations) != 2 || operations[0].OperationID != "contactus.contacts.list" {
		t.Fatalf("operations = %#v", operations)
	}

	schema := Capability()
	var schemaOut bytes.Buffer
	schema.SetOut(&schemaOut)
	schema.SetArgs([]string{"schema", "contactus.contacts.list", "--json"})
	if err := schema.Execute(); err != nil {
		t.Fatalf("capability schema: %v", err)
	}
	var gotSchema capabilitySchema
	if err := json.Unmarshal(schemaOut.Bytes(), &gotSchema); err != nil {
		t.Fatalf("decode schema: %v\n%s", err, schemaOut.String())
	}
	if gotSchema.Result != "ContactList" || len(gotSchema.Inputs) != 1 || gotSchema.Inputs[0].Schema != "ContactListInput" {
		t.Fatalf("schema = %#v", gotSchema)
	}

	help := Capability()
	var helpOut bytes.Buffer
	help.SetOut(&helpOut)
	help.SetArgs([]string{"help", "contactus.contacts.list", "--json"})
	if err := help.Execute(); err != nil {
		t.Fatalf("capability help: %v", err)
	}
	var gotHelp capabilityHelp
	if err := json.Unmarshal(helpOut.Bytes(), &gotHelp); err != nil {
		t.Fatalf("decode help metadata: %v\n%s", err, helpOut.String())
	}
	if gotHelp.CommandPath != "contact list" || len(gotHelp.Aliases) != 2 || gotHelp.Formats[0] != "json" {
		t.Fatalf("help metadata = %#v", gotHelp)
	}
}
