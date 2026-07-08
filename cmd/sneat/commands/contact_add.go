package commands

import (
	"context"
	"fmt"

	"github.com/sneat-co/contactus-ext/backend/contactusmodels/briefs4contactus"
	"github.com/sneat-co/contactus/backend/dto4contactus"
	"github.com/sneat-co/sneat-core-modules/spaceus/dto4spaceus"
	"github.com/sneat-co/sneat-go-core/coretypes"
	"github.com/sneat-co/sneat-go-core/models/dbmodels"
	"github.com/spf13/cobra"
	"github.com/strongo/strongoapp/person"
	"github.com/strongo/strongoapp/with"
)

// ContactWriter performs contact mutations against the sneat-go API.
type ContactWriter interface {
	CreateContact(ctx context.Context, req dto4contactus.CreateContactRequest) (map[string]any, error)
	DeleteContact(ctx context.Context, req dto4contactus.ContactRequest) error
}

// contactInput collects the fields for a new contact (from flags or the form).
type contactInput struct {
	Name, First, Last string
	Gender, Type      string
	AgeGroup          string
	Roles             []string
	Emails            []string
	Phones            []string
}

func (in contactInput) hasAnyField() bool {
	return in.Name != "" || in.First != "" || in.Last != "" || in.Gender != "" ||
		len(in.Roles) > 0 || len(in.Emails) > 0 || len(in.Phones) > 0
}

func (in contactInput) hasName() bool {
	return in.Name != "" || in.First != "" || in.Last != ""
}

func contactAdd(env Env) *cobra.Command {
	in := &contactInput{}
	var space string
	var interactive bool
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a contact (interactive form when run with no field flags)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			spaceID, err := resolveSpaceID(cmd, env, space)
			if err != nil {
				return err
			}
			if interactive || !in.hasAnyField() {
				if env.IsTerminal != nil && !env.IsTerminal() {
					return fmt.Errorf("no contact fields provided and not a terminal; pass --name/--email/… or run interactively")
				}
				if err := env.RunContactForm(in); err != nil {
					return err
				}
			}
			if !in.hasName() {
				return fmt.Errorf("a name is required (use --name or the interactive form)")
			}
			writer, err := env.NewContactWriter(configFromCmd(cmd, env.Getenv))
			if err != nil {
				return err
			}
			resp, err := writer.CreateContact(cmd.Context(), buildCreateContactRequest(spaceID, in))
			if err != nil {
				return err
			}
			return writeJSON(cmd.OutOrStdout(), resp)
		},
	}
	f := cmd.Flags()
	f.StringVar(&space, "space", "", "space id, or 'family'/'private' (default: current space or family)")
	f.StringVar(&in.Name, "name", "", "full name")
	f.StringVar(&in.First, "first", "", "first name")
	f.StringVar(&in.Last, "last", "", "last name")
	f.StringVar(&in.Gender, "gender", "", "male|female|other")
	f.StringVar(&in.AgeGroup, "age-group", "", "unknown|adult|child|senior|undisclosed (default: unknown)")
	f.StringVar(&in.Type, "type", "person", "contact type (person)")
	f.StringArrayVar(&in.Roles, "role", nil, "role (repeatable)")
	f.StringArrayVar(&in.Emails, "email", nil, "email (repeatable)")
	f.StringArrayVar(&in.Phones, "phone", nil, "phone (repeatable)")
	f.BoolVarP(&interactive, "interactive", "i", false, "force the interactive form")
	return cmd
}

func buildCreateContactRequest(spaceID string, in *contactInput) dto4contactus.CreateContactRequest {
	ctype := in.Type
	if ctype == "" {
		ctype = "person"
	}
	names := &person.NameFields{FullName: in.Name, FirstName: in.First, LastName: in.Last}
	ageGroup := in.AgeGroup
	if ageGroup == "" {
		ageGroup = dbmodels.AgeGroupUnknown
	}
	return dto4contactus.CreateContactRequest{
		SpaceRequest: dto4spaceus.SpaceRequest{SpaceID: coretypes.SpaceID(spaceID)},
		Type:         briefs4contactus.ContactType(ctype),
		Status:       "active",
		RolesField:   with.RolesField{Roles: in.Roles},
		Person: &dto4contactus.CreatePersonRequest{
			ContactBase: briefs4contactus.ContactBase{
				ContactBrief: briefs4contactus.ContactBrief{
					Type:     briefs4contactus.ContactType(ctype),
					Gender:   dbmodels.Gender(in.Gender),
					Names:    names,
					AgeGroup: ageGroup,
				},
				Status: "active",
			},
		},
		EmailsField: with.EmailsField{Emails: commChannels(in.Emails)},
		PhonesField: with.PhonesField{Phones: commChannels(in.Phones)},
	}
}

func commChannels(values []string) map[string]*with.CommunicationChannelProps {
	if len(values) == 0 {
		return nil
	}
	m := make(map[string]*with.CommunicationChannelProps, len(values))
	for _, v := range values {
		if v != "" {
			m[v] = &with.CommunicationChannelProps{}
		}
	}
	return m
}
