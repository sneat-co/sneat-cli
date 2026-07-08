package commands

import "github.com/charmbracelet/huh"

// RunContactForm collects contact fields interactively via a huh form.
// It is wired as Env.RunContactForm in production; tests inject a fake.
func RunContactForm(in *contactInput) error {
	if in.Type == "" {
		in.Type = "person"
	}
	email := firstOf(in.Emails)
	phone := firstOf(in.Phones)
	role := firstOf(in.Roles)

	form := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Full name").Value(&in.Name),
		huh.NewSelect[string]().Title("Gender").Options(
			huh.NewOption("— unspecified —", ""),
			huh.NewOption("Male", "male"),
			huh.NewOption("Female", "female"),
			huh.NewOption("Other", "other"),
		).Value(&in.Gender),
		huh.NewInput().Title("Email").Value(&email),
		huh.NewInput().Title("Phone").Value(&phone),
		huh.NewInput().Title("Role").Placeholder("member").Value(&role),
	))
	if err := form.Run(); err != nil {
		return err
	}
	in.Emails = nonEmptySlice(email)
	in.Phones = nonEmptySlice(phone)
	in.Roles = nonEmptySlice(role)
	return nil
}

func firstOf(s []string) string {
	if len(s) > 0 {
		return s[0]
	}
	return ""
}

func nonEmptySlice(v string) []string {
	if v == "" {
		return nil
	}
	return []string{v}
}
