package autocomplete

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/francescoalemanno/raijin-mono/libtui/pkg/fuzzy"
)

// RuneIndexToByteOffset converts a rune index to a byte offset in a string.
// This is necessary because Go strings are byte-indexed, but cursor positions
// are typically tracked as rune indices to handle multi-byte UTF-8 characters.
func RuneIndexToByteOffset(s string, runeIdx int) int {
	if runeIdx <= 0 {
		return 0
	}
	runes := []rune(s)
	if runeIdx >= len(runes) {
		return len(s)
	}
	offset := 0
	for i := 0; i < runeIdx && i < len(runes); i++ {
		offset += len(string(runes[i]))
	}
	return offset
}

// PathDelimiters are characters that delimit path tokens
var PathDelimiters = map[rune]bool{
	' ':  true,
	'\t': true,
	'"':  true,
	'\'': true,
	'=':  true,
}

// findLastDelimiter finds the last delimiter position in text
func findLastDelimiter(text string) int {
	runes := []rune(text)
	for i := len(runes) - 1; i >= 0; i-- {
		if PathDelimiters[runes[i]] {
			return i
		}
	}
	return -1
}

// findUnclosedQuoteStart finds the position of an unclosed quote
func findUnclosedQuoteStart(text string) int {
	inQuotes := false
	quoteStart := -1

	for i, ch := range text {
		if ch == '"' {
			inQuotes = !inQuotes
			if inQuotes {
				quoteStart = i
			}
		}
	}

	if inQuotes {
		return quoteStart
	}
	return -1
}

// isTokenStart checks if index is at the start of a token
func isTokenStart(text string, index int) bool {
	if index == 0 {
		return true
	}
	return PathDelimiters[[]rune(text)[index-1]]
}

// extractQuotedPrefix extracts the prefix from quoted text
func extractQuotedPrefix(text string) string {
	quoteStart := findUnclosedQuoteStart(text)
	if quoteStart == -1 {
		return ""
	}

	if quoteStart > 0 && text[quoteStart-1] == '@' {
		if !isTokenStart(text, quoteStart-1) {
			return ""
		}
		return text[quoteStart-1:]
	}

	if !isTokenStart(text, quoteStart) {
		return ""
	}

	return text[quoteStart:]
}

// ParsedPathPrefix contains parsed path information
type ParsedPathPrefix struct {
	RawPrefix      string
	IsAtPrefix     bool
	IsQuotedPrefix bool
}

// parsePathPrefix parses a path prefix
func parsePathPrefix(prefix string) ParsedPathPrefix {
	if strings.HasPrefix(prefix, `@"`) {
		return ParsedPathPrefix{
			RawPrefix:      prefix[2:],
			IsAtPrefix:     true,
			IsQuotedPrefix: true,
		}
	}
	if strings.HasPrefix(prefix, `"`) {
		return ParsedPathPrefix{
			RawPrefix:      prefix[1:],
			IsAtPrefix:     false,
			IsQuotedPrefix: true,
		}
	}
	if strings.HasPrefix(prefix, "@") {
		return ParsedPathPrefix{
			RawPrefix:      prefix[1:],
			IsAtPrefix:     true,
			IsQuotedPrefix: false,
		}
	}
	return ParsedPathPrefix{
		RawPrefix:      prefix,
		IsAtPrefix:     false,
		IsQuotedPrefix: false,
	}
}

// buildCompletionValue builds a completion value with proper quoting
func buildCompletionValue(path string, isDirectory, isAtPrefix, isQuotedPrefix bool) string {
	needsQuotes := isQuotedPrefix || strings.Contains(path, " ")
	prefix := ""
	if isAtPrefix {
		prefix = "@"
	}

	if !needsQuotes {
		return prefix + path
	}

	return prefix + `"` + path + `"`
}

// AutocompleteItem represents an autocomplete suggestion
type AutocompleteItem struct {
	Value       string
	Label       string
	Description string
}

// SlashCommand represents a slash command
type SlashCommand struct {
	Name                   string
	Description            string
	GetArgumentCompletions func(argumentPrefix string) []AutocompleteItem
}

// SuggestionsResult contains autocomplete suggestions
type SuggestionsResult struct {
	Items  []AutocompleteItem
	Prefix string
}

// AutocompleteProvider is the interface for providing autocomplete suggestions.
type AutocompleteProvider interface {
	// GetSuggestions returns autocomplete suggestions for the current text/cursor position.
	// Returns nil if no suggestions available.
	GetSuggestions(lines []string, cursorLine, cursorCol int) *SuggestionsResult

	// ApplyCompletion applies the selected completion item.
	// Returns the new lines and cursor position.
	ApplyCompletion(lines []string, cursorLine, cursorCol int, item AutocompleteItem, prefix string) (newLines []string, newCursorLine, newCursorCol int)
}

// SkillItem represents a skill that can be triggered with $
type SkillItem struct {
	Name        string
	Description string
}

// CombinedAutocompleteProvider provides autocomplete for slash commands, file paths, and skills
type CombinedAutocompleteProvider struct {
	commands []any // SlashCommand or AutocompleteItem
	skills   []SkillItem
	basePath string
}

// NewCombinedAutocompleteProvider creates a new autocomplete provider
func NewCombinedAutocompleteProvider(commands []any, basePath string) *CombinedAutocompleteProvider {
	if basePath == "" {
		basePath, _ = os.Getwd()
	}
	return &CombinedAutocompleteProvider{
		commands: commands,
		basePath: basePath,
	}
}

// SetSkills sets the available skills for $ completion
func (p *CombinedAutocompleteProvider) SetSkills(skills []SkillItem) {
	p.skills = skills
}

// GetSuggestions gets autocomplete suggestions
// cursorCol is a rune index (not byte offset), which is converted internally
// to handle multi-byte UTF-8 characters correctly.
func (p *CombinedAutocompleteProvider) GetSuggestions(lines []string, cursorLine, cursorCol int) *SuggestionsResult {
	return p.GetSuggestionsWithContext(context.Background(), lines, cursorLine, cursorCol)
}

// GetSuggestionsWithContext gets autocomplete suggestions with a context for cancellation.
// cursorCol is a rune index (not byte offset), which is converted internally
// to handle multi-byte UTF-8 characters correctly.
func (p *CombinedAutocompleteProvider) GetSuggestionsWithContext(ctx context.Context, lines []string, cursorLine, cursorCol int) *SuggestionsResult {
	currentLine := ""
	if cursorLine < len(lines) {
		currentLine = lines[cursorLine]
	}
	textBeforeCursor := ""
	byteOffset := RuneIndexToByteOffset(currentLine, cursorCol)
	if byteOffset <= len(currentLine) {
		textBeforeCursor = currentLine[:byteOffset]
	}

	// Check for $ skill reference
	dollarPrefix := p.extractDollarPrefix(textBeforeCursor)
	if dollarPrefix != "" {
		query := dollarPrefix[1:] // remove $
		suggestions := p.getSkillSuggestions(query)
		if len(suggestions) == 0 {
			return nil
		}
		return &SuggestionsResult{
			Items:  suggestions,
			Prefix: dollarPrefix,
		}
	}

	// Check for @ file reference
	atPrefix := p.extractAtPrefix(textBeforeCursor)
	if atPrefix != "" {
		parsed := parsePathPrefix(atPrefix)
		suggestions := p.getFuzzyFileSuggestions(ctx, parsed.RawPrefix, parsed.IsQuotedPrefix)
		if len(suggestions) == 0 {
			return nil
		}
		return &SuggestionsResult{
			Items:  suggestions,
			Prefix: atPrefix,
		}
	}

	// Check for slash commands
	if strings.HasPrefix(textBeforeCursor, "/") {
		spaceIndex := strings.Index(textBeforeCursor, " ")

		if spaceIndex == -1 {
			// Complete command names
			prefix := textBeforeCursor[1:] // Remove "/"
			return p.getCommandSuggestions(prefix, textBeforeCursor)
		}

		// Complete command arguments
		commandName := textBeforeCursor[1:spaceIndex]
		argumentText := textBeforeCursor[spaceIndex+1:]
		return p.getArgumentSuggestions(commandName, argumentText, textBeforeCursor)
	}

	// Check for file paths
	pathMatch := p.extractPathPrefix(textBeforeCursor, false)
	if pathMatch != "" {
		suggestions := p.getFileSuggestions(pathMatch)
		if len(suggestions) == 0 {
			return nil
		}
		return &SuggestionsResult{
			Items:  suggestions,
			Prefix: pathMatch,
		}
	}

	return nil
}

// getCommandSuggestions gets suggestions for slash commands
func (p *CombinedAutocompleteProvider) getCommandSuggestions(prefix, fullPrefix string) *SuggestionsResult {
	type commandItem struct {
		name        string
		label       string
		description string
	}
	var commandItems []commandItem
	for _, cmd := range p.commands {
		switch c := cmd.(type) {
		case SlashCommand:
			commandItems = append(commandItems, commandItem{name: c.Name, label: c.Name, description: c.Description})
		case AutocompleteItem:
			commandItems = append(commandItems, commandItem{name: c.Value, label: c.Label, description: c.Description})
		}
	}

	filtered := fuzzy.FuzzyFilter(commandItems, prefix, func(item commandItem) string {
		return item.name
	})

	if len(filtered) == 0 {
		return nil
	}

	var items []AutocompleteItem
	for _, ci := range filtered {
		item := AutocompleteItem{
			Value: ci.name,
			Label: ci.label,
		}
		if ci.description != "" {
			item.Description = ci.description
		}
		items = append(items, item)
	}

	return &SuggestionsResult{
		Items:  items,
		Prefix: fullPrefix,
	}
}

// getArgumentSuggestions gets suggestions for command arguments
func (p *CombinedAutocompleteProvider) getArgumentSuggestions(commandName, argumentText, fullPrefix string) *SuggestionsResult {
	for _, cmd := range p.commands {
		switch c := cmd.(type) {
		case SlashCommand:
			if c.Name == commandName && c.GetArgumentCompletions != nil {
				suggestions := c.GetArgumentCompletions(argumentText)
				if len(suggestions) == 0 {
					return nil
				}
				return &SuggestionsResult{
					Items:  suggestions,
					Prefix: argumentText,
				}
			}
		}
	}
	return nil
}

// ApplyCompletion applies a completion
// cursorCol is a rune index (not byte offset).
func (p *CombinedAutocompleteProvider) ApplyCompletion(lines []string, cursorLine, cursorCol int, item AutocompleteItem, prefix string) (newLines []string, newCursorLine, newCursorCol int) {
	currentLine := ""
	if cursorLine < len(lines) {
		currentLine = lines[cursorLine]
	}

	// Convert rune index to byte offset for string slicing
	byteOffset := RuneIndexToByteOffset(currentLine, cursorCol)
	// Calculate byte offset for the prefix start
	prefixStartRuneIdx := max(cursorCol-len([]rune(prefix)), 0)
	prefixStartByteOffset := RuneIndexToByteOffset(currentLine, prefixStartRuneIdx)

	beforePrefix := ""
	if prefixStartByteOffset >= 0 && prefixStartByteOffset <= len(currentLine) {
		beforePrefix = currentLine[:prefixStartByteOffset]
	}

	afterCursor := ""
	if byteOffset <= len(currentLine) {
		afterCursor = currentLine[byteOffset:]
	}

	isQuotedPrefix := strings.HasPrefix(prefix, `"`) || strings.HasPrefix(prefix, `@"`)
	hasLeadingQuoteAfterCursor := strings.HasPrefix(afterCursor, `"`)
	hasTrailingQuoteInItem := strings.HasSuffix(item.Value, `"`)

	adjustedAfterCursor := afterCursor
	if isQuotedPrefix && hasTrailingQuoteInItem && hasLeadingQuoteAfterCursor {
		if len(afterCursor) > 0 {
			adjustedAfterCursor = afterCursor[1:]
		}
	}

	// Check for slash command completion
	isSlashCommand := strings.HasPrefix(prefix, "/") && strings.TrimSpace(beforePrefix) == "" && !strings.Contains(prefix[1:], "/")
	if isSlashCommand {
		value := item.Value
		if !strings.HasPrefix(value, "/") {
			value = "/" + value
		}
		newLine := beforePrefix + value + " " + adjustedAfterCursor
		newLines = make([]string, len(lines))
		copy(newLines, lines)
		newLines[cursorLine] = newLine
		return newLines, cursorLine, len(beforePrefix) + len(value) + 1 // +1 for trailing space
	}

	// Check for skill completion
	if strings.HasPrefix(prefix, "$") {
		newLine := beforePrefix + "$" + item.Value + " " + adjustedAfterCursor
		newLines = make([]string, len(lines))
		copy(newLines, lines)
		newLines[cursorLine] = newLine
		return newLines, cursorLine, len(beforePrefix) + len(item.Value) + 2 // +2 for "$" and space
	}

	// Check for file attachment completion
	if strings.HasPrefix(prefix, "@") {
		isDirectory := strings.HasSuffix(item.Label, "/")
		suffix := ""
		if !isDirectory {
			suffix = " "
		}
		newLine := beforePrefix + item.Value + suffix + adjustedAfterCursor
		newLines = make([]string, len(lines))
		copy(newLines, lines)
		newLines[cursorLine] = newLine

		hasTrailingQuote := strings.HasSuffix(item.Value, `"`)
		cursorOffset := len(item.Value)
		if isDirectory && hasTrailingQuote {
			cursorOffset = len(item.Value) - 1
		}

		return newLines, cursorLine, len(beforePrefix) + cursorOffset + len(suffix)
	}

	// Default file path completion
	newLine := beforePrefix + item.Value + adjustedAfterCursor
	newLines = make([]string, len(lines))
	copy(newLines, lines)
	newLines[cursorLine] = newLine

	isDirectory := strings.HasSuffix(item.Label, "/")
	hasTrailingQuote := strings.HasSuffix(item.Value, `"`)
	cursorOffset := len(item.Value)
	if isDirectory && hasTrailingQuote {
		cursorOffset = len(item.Value) - 1
	}

	return newLines, cursorLine, len(beforePrefix) + cursorOffset
}

// extractAtPrefix extracts @ prefix for fuzzy file suggestions
func (p *CombinedAutocompleteProvider) extractAtPrefix(text string) string {
	quotedPrefix := extractQuotedPrefix(text)
	if strings.HasPrefix(quotedPrefix, `@"`) {
		return quotedPrefix
	}

	lastDelimiterRuneIdx := findLastDelimiter(text)
	tokenStartRuneIdx := 0
	if lastDelimiterRuneIdx != -1 {
		tokenStartRuneIdx = lastDelimiterRuneIdx + 1
	}

	// Convert rune index to byte offset
	tokenStartByte := RuneIndexToByteOffset(text, tokenStartRuneIdx)

	if tokenStartByte < len(text) && text[tokenStartByte] == '@' {
		return text[tokenStartByte:]
	}

	return ""
}

// extractDollarPrefix extracts $ prefix for skill suggestions
func (p *CombinedAutocompleteProvider) extractDollarPrefix(text string) string {
	lastDelimiterRuneIdx := findLastDelimiter(text)
	tokenStartRuneIdx := 0
	if lastDelimiterRuneIdx != -1 {
		tokenStartRuneIdx = lastDelimiterRuneIdx + 1
	}

	// Convert rune index to byte offset
	tokenStartByte := RuneIndexToByteOffset(text, tokenStartRuneIdx)

	if tokenStartByte < len(text) && text[tokenStartByte] == '$' {
		return text[tokenStartByte:]
	}

	return ""
}

// getSkillSuggestions returns skill suggestions matching the query
func (p *CombinedAutocompleteProvider) getSkillSuggestions(query string) []AutocompleteItem {
	if len(p.skills) == 0 {
		return nil
	}

	var items []AutocompleteItem
	for _, s := range p.skills {
		items = append(items, AutocompleteItem{
			Value:       s.Name,
			Label:       s.Name,
			Description: s.Description,
		})
	}

	if query != "" {
		items = fuzzy.FuzzyFilter(items, query, func(item AutocompleteItem) string {
			return item.Value
		})
	}

	return items
}

// extractPathPrefix extracts a path-like prefix from text
func (p *CombinedAutocompleteProvider) extractPathPrefix(text string, forceExtract bool) string {
	quotedPrefix := extractQuotedPrefix(text)
	if quotedPrefix != "" {
		return quotedPrefix
	}

	lastDelimiterIndex := findLastDelimiter(text)
	pathPrefix := text
	if lastDelimiterIndex != -1 {
		pathPrefix = text[lastDelimiterIndex+1:]
	}

	if forceExtract {
		return pathPrefix
	}

	// Natural triggers
	if strings.Contains(pathPrefix, "/") || strings.HasPrefix(pathPrefix, ".") || strings.HasPrefix(pathPrefix, "~/") {
		return pathPrefix
	}

	if pathPrefix == "" && strings.HasSuffix(text, " ") {
		return pathPrefix
	}

	return ""
}

// expandHomePath expands ~ to home directory
func expandHomePath(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, _ := os.UserHomeDir()
		expanded := filepath.Join(home, path[2:])
		if strings.HasSuffix(path, "/") && !strings.HasSuffix(expanded, "/") {
			return expanded + "/"
		}
		return expanded
	}
	if path == "~" {
		home, _ := os.UserHomeDir()
		return home
	}
	return path
}

// getFileSuggestions gets file/directory suggestions
func (p *CombinedAutocompleteProvider) getFileSuggestions(prefix string) []AutocompleteItem {
	parsed := parsePathPrefix(prefix)
	expandedPrefix := parsed.RawPrefix

	// Handle home directory expansion
	if strings.HasPrefix(expandedPrefix, "~") {
		expandedPrefix = expandHomePath(expandedPrefix)
	}

	var searchDir string
	var searchPrefix string

	// Determine search directory and prefix
	isRootPrefix := parsed.RawPrefix == "" ||
		parsed.RawPrefix == "./" ||
		parsed.RawPrefix == "../" ||
		parsed.RawPrefix == "~" ||
		parsed.RawPrefix == "~/" ||
		parsed.RawPrefix == "/" ||
		(parsed.IsAtPrefix && parsed.RawPrefix == "")

	if isRootPrefix {
		if strings.HasPrefix(parsed.RawPrefix, "~") || strings.HasPrefix(expandedPrefix, "/") {
			searchDir = expandedPrefix
		} else {
			searchDir = filepath.Join(p.basePath, expandedPrefix)
		}
		searchPrefix = ""
	} else if strings.HasSuffix(parsed.RawPrefix, "/") {
		if strings.HasPrefix(parsed.RawPrefix, "~") || strings.HasPrefix(expandedPrefix, "/") {
			searchDir = expandedPrefix
		} else {
			searchDir = filepath.Join(p.basePath, expandedPrefix)
		}
		searchPrefix = ""
	} else {
		dir := filepath.Dir(expandedPrefix)
		file := filepath.Base(expandedPrefix)
		if strings.HasPrefix(parsed.RawPrefix, "~") || strings.HasPrefix(expandedPrefix, "/") {
			searchDir = dir
		} else {
			searchDir = filepath.Join(p.basePath, dir)
		}
		searchPrefix = file
	}

	entries, err := os.ReadDir(searchDir)
	if err != nil {
		return nil
	}

	var suggestions []AutocompleteItem
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(strings.ToLower(name), strings.ToLower(searchPrefix)) {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			continue
		}

		isDirectory := info.IsDir()

		// Build relative path
		var relativePath string
		displayPrefix := parsed.RawPrefix

		if strings.HasSuffix(displayPrefix, "/") {
			relativePath = displayPrefix + name
		} else if strings.Contains(displayPrefix, "/") {
			if strings.HasPrefix(displayPrefix, "~/") {
				homeRelativeDir := displayPrefix[2:]
				dir := filepath.Dir(homeRelativeDir)
				if dir == "." {
					relativePath = "~/" + name
				} else {
					relativePath = "~/" + filepath.Join(dir, name)
				}
			} else if strings.HasPrefix(displayPrefix, "/") {
				dir := filepath.Dir(displayPrefix)
				if dir == "/" {
					relativePath = "/" + name
				} else {
					relativePath = dir + "/" + name
				}
			} else {
				relativePath = filepath.Join(filepath.Dir(displayPrefix), name)
			}
		} else {
			if strings.HasPrefix(displayPrefix, "~") {
				relativePath = "~/" + name
			} else {
				relativePath = name
			}
		}

		pathValue := relativePath
		if isDirectory {
			pathValue = relativePath + "/"
		}

		value := buildCompletionValue(pathValue, isDirectory, parsed.IsAtPrefix, parsed.IsQuotedPrefix)

		label := name
		if isDirectory {
			label = name + "/"
		}

		suggestions = append(suggestions, AutocompleteItem{
			Value: value,
			Label: label,
		})
	}

	// Sort directories first, then alphabetically
	sort.Slice(suggestions, func(i, j int) bool {
		aIsDir := strings.HasSuffix(suggestions[i].Value, "/") || strings.HasSuffix(strings.Trim(suggestions[i].Value, `"@`), "/")
		bIsDir := strings.HasSuffix(suggestions[j].Value, "/") || strings.HasSuffix(strings.Trim(suggestions[j].Value, `"@`), "/")
		if aIsDir && !bIsDir {
			return true
		}
		if !aIsDir && bIsDir {
			return false
		}
		return suggestions[i].Label < suggestions[j].Label
	})

	return suggestions
}

type walkEntry struct {
	Path        string
	IsDirectory bool
}

// walkDirectory recursively walks a directory tree, returning files and directories.
// It skips .git directories and collects up to maxResults entries.
// The context can be cancelled to abort the walk early.
func walkDirectory(ctx context.Context, baseDir, query string, maxResults int) []walkEntry {
	lowerQuery := strings.ToLower(query)
	var results []walkEntry

	filepath.WalkDir(baseDir, func(path string, d fs.DirEntry, err error) error {
		// Check for cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err != nil {
			return nil
		}
		if len(results) >= maxResults {
			return fs.SkipAll
		}

		rel, err := filepath.Rel(baseDir, path)
		if err != nil || rel == "." {
			return nil
		}

		name := d.Name()
		if name == ".git" && d.IsDir() {
			return fs.SkipDir
		}

		if lowerQuery != "" && !strings.Contains(strings.ToLower(rel), lowerQuery) {
			return nil
		}

		entry := rel
		if d.IsDir() {
			entry += "/"
		}
		results = append(results, walkEntry{Path: entry, IsDirectory: d.IsDir()})
		return nil
	})

	return results
}

// scoreEntry scores an entry against the query (higher = better match)
func (p *CombinedAutocompleteProvider) scoreEntry(filePath, query string, isDirectory bool) int {
	fileName := filepath.Base(filePath)
	lowerFileName := strings.ToLower(fileName)
	lowerQuery := strings.ToLower(query)

	score := 0

	if lowerFileName == lowerQuery {
		score = 100
	} else if strings.HasPrefix(lowerFileName, lowerQuery) {
		score = 80
	} else if strings.Contains(lowerFileName, lowerQuery) {
		score = 50
	} else if strings.Contains(strings.ToLower(filePath), lowerQuery) {
		score = 30
	}

	if isDirectory && score > 0 {
		score += 10
	}

	return score
}

// resolveScopedFuzzyQuery resolves a scoped fuzzy query like "src/foo" into baseDir + query
func (p *CombinedAutocompleteProvider) resolveScopedFuzzyQuery(rawQuery string) *struct {
	BaseDir     string
	Query       string
	DisplayBase string
} {
	slashIndex := strings.LastIndex(rawQuery, "/")
	if slashIndex == -1 {
		return nil
	}

	displayBase := rawQuery[:slashIndex+1]
	query := rawQuery[slashIndex+1:]

	var baseDir string
	if strings.HasPrefix(displayBase, "~/") {
		baseDir = expandHomePath(displayBase)
	} else if strings.HasPrefix(displayBase, "/") {
		baseDir = displayBase
	} else {
		baseDir = filepath.Join(p.basePath, displayBase)
	}

	info, err := os.Stat(baseDir)
	if err != nil || !info.IsDir() {
		return nil
	}

	return &struct {
		BaseDir     string
		Query       string
		DisplayBase string
	}{BaseDir: baseDir, Query: query, DisplayBase: displayBase}
}

// scopedPathForDisplay builds the display path for a scoped query result
func scopedPathForDisplay(displayBase, relativePath string) string {
	if displayBase == "/" {
		return "/" + relativePath
	}
	return displayBase + relativePath
}

// getFuzzyFileSuggestions gets fuzzy file suggestions using fd
// The context can be cancelled to abort the search early.
func (p *CombinedAutocompleteProvider) getFuzzyFileSuggestions(ctx context.Context, query string, isQuotedPrefix bool) []AutocompleteItem {
	scopedQuery := p.resolveScopedFuzzyQuery(query)
	walkBaseDir := p.basePath
	walkQuery := query
	if scopedQuery != nil {
		walkBaseDir = scopedQuery.BaseDir
		walkQuery = scopedQuery.Query
	}

	entries := walkDirectory(ctx, walkBaseDir, walkQuery, 100)

	// Score entries
	type scoredEntry struct {
		path        string
		isDirectory bool
		score       int
	}
	var scored []scoredEntry
	for _, e := range entries {
		s := 1
		if walkQuery != "" {
			s = p.scoreEntry(e.Path, walkQuery, e.IsDirectory)
		}
		if s > 0 {
			scored = append(scored, scoredEntry{path: e.Path, isDirectory: e.IsDirectory, score: s})
		}
	}

	// Sort by score descending, take top 20
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].score > scored[j].score
	})
	if len(scored) > 20 {
		scored = scored[:20]
	}

	var suggestions []AutocompleteItem
	for _, se := range scored {
		pathWithoutSlash := se.path
		if se.isDirectory {
			pathWithoutSlash = se.path[:len(se.path)-1]
		}
		displayPath := pathWithoutSlash
		if scopedQuery != nil {
			displayPath = scopedPathForDisplay(scopedQuery.DisplayBase, pathWithoutSlash)
		}
		entryName := filepath.Base(pathWithoutSlash)
		completionPath := displayPath
		if se.isDirectory {
			completionPath = displayPath + "/"
		}
		value := buildCompletionValue(completionPath, se.isDirectory, true, isQuotedPrefix)

		label := entryName
		if se.isDirectory {
			label = entryName + "/"
		}

		suggestions = append(suggestions, AutocompleteItem{
			Value:       value,
			Label:       label,
			Description: displayPath,
		})
	}

	return suggestions
}

// GetForceFileSuggestions forces file completion (called on Tab key)
// cursorCol is a rune index (not byte offset).
func (p *CombinedAutocompleteProvider) GetForceFileSuggestions(lines []string, cursorLine, cursorCol int) *SuggestionsResult {
	currentLine := ""
	if cursorLine < len(lines) {
		currentLine = lines[cursorLine]
	}
	textBeforeCursor := ""
	byteOffset := RuneIndexToByteOffset(currentLine, cursorCol)
	if byteOffset <= len(currentLine) {
		textBeforeCursor = currentLine[:byteOffset]
	}

	// Don't trigger for slash commands at start of line
	trimmed := strings.TrimSpace(textBeforeCursor)
	if strings.HasPrefix(trimmed, "/") && !strings.Contains(trimmed, " ") {
		return nil
	}

	pathMatch := p.extractPathPrefix(textBeforeCursor, true)
	if pathMatch != "" {
		suggestions := p.getFileSuggestions(pathMatch)
		if len(suggestions) == 0 {
			return nil
		}
		return &SuggestionsResult{
			Items:  suggestions,
			Prefix: pathMatch,
		}
	}

	return nil
}

// ShouldTriggerFileCompletion checks if file completion should trigger
// cursorCol is a rune index (not byte offset).
func (p *CombinedAutocompleteProvider) ShouldTriggerFileCompletion(lines []string, cursorLine, cursorCol int) bool {
	currentLine := ""
	if cursorLine < len(lines) {
		currentLine = lines[cursorLine]
	}
	textBeforeCursor := ""
	byteOffset := RuneIndexToByteOffset(currentLine, cursorCol)
	if byteOffset <= len(currentLine) {
		textBeforeCursor = currentLine[:byteOffset]
	}

	trimmed := strings.TrimSpace(textBeforeCursor)
	if strings.HasPrefix(trimmed, "/") && !strings.Contains(trimmed, " ") {
		return false
	}

	return true
}
