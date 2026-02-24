package chat

import (
	"github.com/francescoalemanno/raijin-mono/libtui/pkg/autocomplete"

	"github.com/francescoalemanno/raijin-mono/internal/prompts"
	"github.com/francescoalemanno/raijin-mono/internal/skills"
)

// ---------------------------------------------------------------------------
// Provider setup — builds a CombinedAutocompleteProvider with commands, skills,
// and file mentions, then sets it on the editor.
// ---------------------------------------------------------------------------

func (app *ChatApp) setupAutocompleteProvider() {
	// Build slash commands from built-in command help.
	var autoItems []interface{}
	reserved := builtinSlashCommands()
	for _, cmd := range commandNamesDescs {
		autoItems = append(autoItems, autocomplete.AutocompleteItem{
			Value:       cmd.Command,
			Label:       cmd.Command,
			Description: cmd.Desc,
		})
	}
	for _, tmpl := range prompts.Load().Templates {
		if _, blocked := reserved[tmpl.Name]; blocked {
			continue
		}
		desc := tmpl.Description
		if desc == "" {
			desc = "(no description)"
		}
		desc += " [" + string(tmpl.Source) + "]"
		autoItems = append(autoItems, autocomplete.AutocompleteItem{
			Value:       "/" + tmpl.Name,
			Label:       "/" + tmpl.Name,
			Description: desc,
		})
	}

	provider := autocomplete.NewCombinedAutocompleteProvider(autoItems, "")

	// Register skills
	available := skills.GetSkills()
	skillItems := make([]autocomplete.SkillItem, len(available))
	for i, s := range available {
		skillItems[i] = autocomplete.SkillItem{
			Name:        s.Name,
			Description: s.Description,
		}
	}
	provider.SetSkills(skillItems)

	app.editor.SetAutocompleteProvider(provider)
}
