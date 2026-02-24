package components

import "testing"

func plainSettingsTheme() SettingsListTheme {
	identity := func(text string, _ bool) string { return text }
	return SettingsListTheme{
		Label:       identity,
		Value:       identity,
		Description: func(text string) string { return text },
		Cursor:      "> ",
		Hint:        func(text string) string { return text },
	}
}

func selectedFilteredID(s *SettingsList) string {
	if s.selectedIndex < 0 || s.selectedIndex >= len(s.filteredItems) {
		return ""
	}
	return s.filteredItems[s.selectedIndex].ID
}

func TestSettingsListApplyFilterKeepsSelectedItemWhenStillVisible(t *testing.T) {
	t.Parallel()

	s := NewSettingsList([]SettingItem{
		{ID: "alpha", Label: "Alpha"},
		{ID: "beta", Label: "Beta"},
		{ID: "gamma", Label: "Gamma"},
	}, 8, plainSettingsTheme(), func(string, string) {}, func() {}, SettingsListOptions{EnableSearch: true})

	s.selectedIndex = 2
	s.lastSelectedID = "gamma"

	s.applyFilter("ga")
	if got := selectedFilteredID(s); got != "gamma" {
		t.Fatalf("selected id after first filter = %q, want %q", got, "gamma")
	}

	s.applyFilter("a")
	if got := selectedFilteredID(s); got != "gamma" {
		t.Fatalf("selected id after refilter = %q, want %q", got, "gamma")
	}
}

func TestSettingsListApplyFilterRestoresSelectionAfterEmptyResult(t *testing.T) {
	t.Parallel()

	s := NewSettingsList([]SettingItem{
		{ID: "alpha", Label: "Alpha"},
		{ID: "beta", Label: "Beta"},
		{ID: "gamma", Label: "Gamma"},
	}, 8, plainSettingsTheme(), func(string, string) {}, func() {}, SettingsListOptions{EnableSearch: true})

	s.selectedIndex = 2
	s.lastSelectedID = "gamma"

	s.applyFilter("zzzz")
	if len(s.filteredItems) != 0 {
		t.Fatalf("expected no filtered items, got %d", len(s.filteredItems))
	}

	s.applyFilter("ga")
	if got := selectedFilteredID(s); got != "gamma" {
		t.Fatalf("selected id after recovering from empty filter = %q, want %q", got, "gamma")
	}
}
