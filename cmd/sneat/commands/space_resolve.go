package commands

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// defaultSpaceType is used when no --space is given and no current space is set.
const defaultSpaceType = "family"

// resolveSpaceID turns a --space value into a real space id:
//   - a real id is returned as-is;
//   - "" falls back to the current space (set by `space use`), then to the
//     family space;
//   - the pseudo ids "family"/"private" resolve to the user's single space of
//     that type (a user has at most one default family and one private space).
func resolveSpaceID(cmd *cobra.Command, env Env, flagValue string) (string, error) {
	candidate := flagValue
	if candidate != "" && candidate != "family" && candidate != "private" {
		return candidate, nil // explicit real id
	}

	sess, err := env.Store.Load()
	if err != nil {
		return "", err
	}
	if candidate == "" {
		candidate = sess.CurrentSpace
		if candidate == "" {
			candidate = defaultSpaceType
		}
	}
	if candidate != "family" && candidate != "private" {
		return candidate, nil // current space was a real id
	}

	reader, err := env.NewSpacesReader(configFromCmd(cmd, env.Getenv))
	if err != nil {
		return "", err
	}
	spaces, err := reader.ListSpaces(cmd.Context(), sess.UID)
	if err != nil {
		return "", err
	}
	return spaceIDByType(spaces, candidate)
}

// spaceIDByType finds the single space id whose brief has the given type.
func spaceIDByType(spaces map[string]any, spaceType string) (string, error) {
	var found []string
	for id, v := range spaces {
		if b, ok := v.(map[string]any); ok && str(b["type"]) == spaceType {
			found = append(found, id)
		}
	}
	switch len(found) {
	case 1:
		return found[0], nil
	case 0:
		return "", fmt.Errorf("no %s space found for the current user; pass --space <id>", spaceType)
	default:
		return "", fmt.Errorf("multiple %s spaces found (%s); pass --space <id>", spaceType, strings.Join(found, ", "))
	}
}
