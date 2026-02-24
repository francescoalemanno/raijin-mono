package test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/autocomplete"
)

func setupFolder(baseDir string, dirs []string, files map[string]string) {
	for _, dir := range dirs {
		os.MkdirAll(filepath.Join(baseDir, dir), 0o755)
	}
	for filePath, content := range files {
		fullPath := filepath.Join(baseDir, filePath)
		os.MkdirAll(filepath.Dir(fullPath), 0o755)
		os.WriteFile(fullPath, []byte(content), 0o644)
	}
}

func TestCombinedAutocompleteProvider_ExtractPathPrefix_Root(t *testing.T) {
	provider := autocomplete.NewCombinedAutocompleteProvider([]interface{}{}, "/tmp")
	lines := []string{"hey /"}

	result := provider.GetForceFileSuggestions(lines, 0, 5)

	assert.NotNil(t, result, "Should return suggestions for root directory")
	if result != nil {
		assert.Equal(t, "/", result.Prefix, "Prefix should be '/'")
	}
}

func TestCombinedAutocompleteProvider_ExtractPathPrefix_PathA(t *testing.T) {
	provider := autocomplete.NewCombinedAutocompleteProvider([]interface{}{}, "/tmp")
	lines := []string{"/A"}

	result := provider.GetForceFileSuggestions(lines, 0, 2)

	// This might return null if /A doesn't match anything, which is fine
	// We're mainly testing that the prefix extraction works
	if result != nil {
		assert.Equal(t, "/A", result.Prefix, "Prefix should be '/A'")
	}
}

func TestCombinedAutocompleteProvider_NoTriggerForSlashCommands(t *testing.T) {
	provider := autocomplete.NewCombinedAutocompleteProvider([]interface{}{}, "/tmp")
	lines := []string{"/model"}

	result := provider.GetForceFileSuggestions(lines, 0, 6)

	assert.Nil(t, result, "Should not trigger for slash commands")
}

func TestCombinedAutocompleteProvider_TriggerAfterSlashCommandArg(t *testing.T) {
	provider := autocomplete.NewCombinedAutocompleteProvider([]interface{}{}, "/tmp")
	lines := []string{"/command /"}

	result := provider.GetForceFileSuggestions(lines, 0, 10)

	assert.NotNil(t, result, "Should trigger for absolute paths in command arguments")
	if result != nil {
		assert.Equal(t, "/", result.Prefix, "Prefix should be '/'")
	}
}

func TestCombinedAutocompleteProvider_EmptyQuery(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "raijin-autocomplete-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	setupFolder(tempDir, []string{"src"}, map[string]string{
		"README.md": "readme",
	})

	provider := autocomplete.NewCombinedAutocompleteProvider([]interface{}{}, tempDir)
	lines := []string{"@"}

	result := provider.GetSuggestions(lines, 0, 1)

	assert.NotNil(t, result, "Should return suggestions for empty @ query")
	if result != nil {
		values := make([]string, len(result.Items))
		for i, item := range result.Items {
			values[i] = item.Value
		}
		assert.Contains(t, values, "@README.md", "Should include README.md")
	}
}

func TestCombinedAutocompleteProvider_WithExtension(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "raijin-autocomplete-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	setupFolder(tempDir, []string{}, map[string]string{
		"file.txt": "content",
	})

	provider := autocomplete.NewCombinedAutocompleteProvider([]interface{}{}, tempDir)
	lines := []string{"@file.txt"}

	result := provider.GetSuggestions(lines, 0, 9)

	assert.NotNil(t, result)
	if result != nil {
		var values []string
		for _, item := range result.Items {
			values = append(values, item.Value)
		}
		assert.Contains(t, values, "@file.txt")
	}
}

func TestCombinedAutocompleteProvider_CaseInsensitive(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "raijin-autocomplete-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	setupFolder(tempDir, []string{"src"}, map[string]string{
		"README.md": "readme",
	})

	provider := autocomplete.NewCombinedAutocompleteProvider([]interface{}{}, tempDir)
	lines := []string{"@re"}

	result := provider.GetSuggestions(lines, 0, 3)

	assert.NotNil(t, result)
	if result != nil {
		var values []string
		for _, item := range result.Items {
			values = append(values, item.Value)
		}
		assert.Contains(t, values, "@README.md")
	}
}

func TestCombinedAutocompleteProvider_DirectoriesFirst(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "raijin-autocomplete-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	setupFolder(tempDir, []string{"src"}, map[string]string{
		"src.txt": "text",
	})

	provider := autocomplete.NewCombinedAutocompleteProvider([]interface{}{}, tempDir)
	lines := []string{"@src"}

	result := provider.GetSuggestions(lines, 0, 4)

	assert.NotNil(t, result)
	if result != nil && len(result.Items) > 0 {
		// Directories should be ranked before files
		firstValue := result.Items[0].Value
		assert.Contains(t, firstValue, "src/", "First item should be the directory")
	}
}

func TestCombinedAutocompleteProvider_NestedPaths(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "raijin-autocomplete-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	setupFolder(tempDir, []string{"src"}, map[string]string{
		"src/index.ts": "export {};\n",
	})

	provider := autocomplete.NewCombinedAutocompleteProvider([]interface{}{}, tempDir)
	lines := []string{"@index"}

	result := provider.GetSuggestions(lines, 0, 6)

	assert.NotNil(t, result)
	if result != nil {
		var values []string
		for _, item := range result.Items {
			values = append(values, item.Value)
		}
		assert.Contains(t, values, "@src/index.ts")
	}
}

func TestCombinedAutocompleteProvider_ImageFiles(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "raijin-autocomplete-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	setupFolder(tempDir, []string{}, map[string]string{
		"photo.jpeg": "fake image data",
		"icon.png":   "fake png data",
	})

	provider := autocomplete.NewCombinedAutocompleteProvider([]interface{}{}, tempDir)
	lines := []string{"@photo"}

	result := provider.GetSuggestions(lines, 0, 6)

	assert.NotNil(t, result)
	if result != nil {
		var values []string
		for _, item := range result.Items {
			values = append(values, item.Value)
		}
		assert.Contains(t, values, "@photo.jpeg", "Should include image files in suggestions")
	}
}

func TestRuneIndexToByteOffset(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		runeIdx  int
		expected int
	}{
		{"empty", "", 0, 0},
		{"empty with index", "", 5, 0},
		{"ASCII", "hello", 3, 3},
		{"ASCII at end", "hello", 5, 5},
		{"Unicode bullet", "•", 1, 3},
		{"Unicode before text", "•abc", 2, 4}, // bullet(3) + 'a'(1) = 4
		{"Unicode at end", "•abc", 4, 6},      // bullet(3) + abc(3) = 6
		{"Chinese", "你好", 1, 3},
		{"Mixed", "•hello", 3, 5}, // bullet(3) + he(2) = 5
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := autocomplete.RuneIndexToByteOffset(tt.input, tt.runeIdx)
			assert.Equal(t, tt.expected, result, "runeIndexToByteOffset(%q, %d)", tt.input, tt.runeIdx)
		})
	}
}

// TestCombinedAutocompleteProvider_UnicodeCharacter tests that autocomplete works
// correctly when there are multi-byte UTF-8 characters before the cursor.
// The cursorCol parameter is a rune index, not a byte index.
func TestCombinedAutocompleteProvider_UnicodeCharacter(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "raijin-autocomplete-test-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	setupFolder(tempDir, []string{}, map[string]string{
		"file.txt": "content",
	})

	provider := autocomplete.NewCombinedAutocompleteProvider([]interface{}{}, tempDir)

	// "•" is a 3-byte UTF-8 character (U+2022)
	// Test with Unicode character followed by space before @
	// Line: "• @file" has 7 runes: bullet + space + @ + f + i + l + e
	// cursorCol should be 7 (after "@file")
	lines := []string{"• @file"}

	// cursorCol is rune index: 7 (end of string)
	result := provider.GetSuggestions(lines, 0, 7)

	assert.NotNil(t, result, "Should return suggestions even with Unicode characters before cursor")
	if result != nil {
		var values []string
		for _, item := range result.Items {
			values = append(values, item.Value)
		}
		assert.Contains(t, values, "@file.txt", "Should complete file after Unicode character")
		assert.Equal(t, "@file", result.Prefix, "Prefix should be the rune-based prefix, not corrupted by byte slicing")
	}
}
