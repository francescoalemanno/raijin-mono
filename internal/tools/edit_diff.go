package tools

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/sergi/go-diff/diffmatchpatch"
)

type fuzzyMatchResult struct {
	Found                 bool
	Index                 int
	MatchLength           int
	UsedFuzzyMatch        bool
	ContentForReplacement string
}

type EditToolDetails struct {
	Diff             string `json:"diff"`
	FirstChangedLine *int   `json:"firstChangedLine,omitempty"`
}

func detectLineEnding(content string) string {
	crlfIdx := strings.Index(content, "\r\n")
	lfIdx := strings.Index(content, "\n")
	if lfIdx == -1 {
		return "\n"
	}
	if crlfIdx == -1 {
		return "\n"
	}
	if crlfIdx < lfIdx {
		return "\r\n"
	}
	return "\n"
}

func normalizeToLF(text string) string {
	return strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
}

func restoreLineEndings(text string, ending string) string {
	if ending == "\r\n" {
		return strings.ReplaceAll(text, "\n", "\r\n")
	}
	return text
}

func normalizeForFuzzyMatch(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRightFunc(line, unicode.IsSpace)
	}
	normalized := strings.Join(lines, "\n")
	return strings.Map(func(r rune) rune {
		switch r {
		// Smart single quotes
		case '\u2018', '\u2019', '\u201A', '\u201B':
			return '\''
		// Smart double quotes
		case '\u201C', '\u201D', '\u201E', '\u201F':
			return '"'
		// Dashes/hyphens/minus variants
		case '\u2010', '\u2011', '\u2012', '\u2013', '\u2014', '\u2015', '\u2212':
			return '-'
		// Special spaces
		case '\u00A0', '\u2002', '\u2003', '\u2004', '\u2005', '\u2006', '\u2007', '\u2008', '\u2009', '\u200A', '\u202F', '\u205F', '\u3000':
			return ' '
		default:
			return r
		}
	}, normalized)
}

func fuzzyFindText(content string, oldText string) fuzzyMatchResult {
	exactIndex := strings.Index(content, oldText)
	if exactIndex != -1 {
		return fuzzyMatchResult{
			Found:                 true,
			Index:                 exactIndex,
			MatchLength:           len(oldText),
			UsedFuzzyMatch:        false,
			ContentForReplacement: content,
		}
	}

	fuzzyContent := normalizeForFuzzyMatch(content)
	fuzzyOldText := normalizeForFuzzyMatch(oldText)
	fuzzyIndex := strings.Index(fuzzyContent, fuzzyOldText)
	if fuzzyIndex == -1 {
		return fuzzyMatchResult{
			Found:                 false,
			Index:                 -1,
			MatchLength:           0,
			UsedFuzzyMatch:        false,
			ContentForReplacement: content,
		}
	}

	return fuzzyMatchResult{
		Found:                 true,
		Index:                 fuzzyIndex,
		MatchLength:           len(fuzzyOldText),
		UsedFuzzyMatch:        true,
		ContentForReplacement: fuzzyContent,
	}
}

func stripBom(content string) (bom string, text string) {
	if strings.HasPrefix(content, "\uFEFF") {
		return "\uFEFF", content[len("\uFEFF"):]
	}
	return "", content
}

func generateDiffString(oldContent string, newContent string, contextLines int) EditToolDetails {
	dmp := diffmatchpatch.New()
	a, b, lineArray := dmp.DiffLinesToChars(oldContent, newContent)
	diffs := dmp.DiffMain(a, b, false)
	diffs = dmp.DiffCharsToLines(diffs, lineArray)

	output := make([]string, 0)
	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")
	maxLineNum := len(oldLines)
	if len(newLines) > maxLineNum {
		maxLineNum = len(newLines)
	}
	lineNumWidth := len(strconv.Itoa(maxLineNum))

	oldLineNum := 1
	newLineNum := 1
	lastWasChange := false
	var firstChangedLine *int

	for i := 0; i < len(diffs); i++ {
		diff := diffs[i]
		raw := strings.Split(diff.Text, "\n")
		if len(raw) > 0 && raw[len(raw)-1] == "" {
			raw = raw[:len(raw)-1]
		}

		if diff.Type == diffmatchpatch.DiffInsert || diff.Type == diffmatchpatch.DiffDelete {
			if firstChangedLine == nil {
				line := newLineNum
				firstChangedLine = &line
			}
			for _, line := range raw {
				if diff.Type == diffmatchpatch.DiffInsert {
					lineNum := fmt.Sprintf("%*d", lineNumWidth, newLineNum)
					output = append(output, fmt.Sprintf("+%s %s", lineNum, line))
					newLineNum++
				} else {
					lineNum := fmt.Sprintf("%*d", lineNumWidth, oldLineNum)
					output = append(output, fmt.Sprintf("-%s %s", lineNum, line))
					oldLineNum++
				}
			}
			lastWasChange = true
			continue
		}

		nextPartIsChange := i < len(diffs)-1 && (diffs[i+1].Type == diffmatchpatch.DiffInsert || diffs[i+1].Type == diffmatchpatch.DiffDelete)
		if lastWasChange || nextPartIsChange {
			linesToShow := raw
			skipStart := 0
			skipEnd := 0

			if !lastWasChange {
				skipStart = max(0, len(raw)-contextLines)
				linesToShow = raw[skipStart:]
			}
			if !nextPartIsChange && len(linesToShow) > contextLines {
				skipEnd = len(linesToShow) - contextLines
				linesToShow = linesToShow[:contextLines]
			}

			if skipStart > 0 {
				output = append(output, fmt.Sprintf(" %s ...", strings.Repeat(" ", lineNumWidth)))
				oldLineNum += skipStart
				newLineNum += skipStart
			}

			for _, line := range linesToShow {
				lineNum := fmt.Sprintf("%*d", lineNumWidth, oldLineNum)
				output = append(output, fmt.Sprintf(" %s %s", lineNum, line))
				oldLineNum++
				newLineNum++
			}

			if skipEnd > 0 {
				output = append(output, fmt.Sprintf(" %s ...", strings.Repeat(" ", lineNumWidth)))
				oldLineNum += skipEnd
				newLineNum += skipEnd
			}
		} else {
			oldLineNum += len(raw)
			newLineNum += len(raw)
		}
		lastWasChange = false
	}

	return EditToolDetails{Diff: strings.Join(output, "\n"), FirstChangedLine: firstChangedLine}
}
