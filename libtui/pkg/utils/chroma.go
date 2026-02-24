package utils

import (
	"bytes"
	"path/filepath"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

var (
	chromaFormatter = formatters.Get("terminal16m")
	chromaStyle     = resolveChromaStyle()
)

func resolveChromaStyle() *chroma.Style {
	if style := styles.Get("nord"); style != nil {
		return style
	}
	return styles.Fallback
}

// HighlightCodeANSI returns ANSI-highlighted code using Chroma.
// If no suitable lexer is found, the original code is returned unchanged.
func HighlightCodeANSI(code, language, filename string) string {
	if code == "" || chromaFormatter == nil {
		return code
	}

	lexer := selectCodeLexer(code, language, filename)
	if lexer == nil {
		return code
	}

	if cfg := lexer.Config(); cfg != nil {
		name := strings.ToLower(cfg.Name)
		if name == "fallback" || name == "plaintext" || name == "text" {
			return code
		}
	}

	iterator, err := chroma.Coalesce(lexer).Tokenise(nil, code)
	if err != nil {
		return code
	}

	var buf bytes.Buffer
	if err := chromaFormatter.Format(&buf, chromaStyle, iterator); err != nil {
		return code
	}

	highlighted := buf.String()
	if strings.HasSuffix(code, "\n") {
		if !strings.HasSuffix(highlighted, "\n") {
			highlighted += "\n"
		}
		return highlighted
	}
	return strings.TrimSuffix(highlighted, "\n")
}

// HighlightCodeLines returns ANSI-highlighted code split by lines.
func HighlightCodeLines(code, language, filename string) []string {
	if code == "" {
		return []string{}
	}
	return strings.Split(HighlightCodeANSI(code, language, filename), "\n")
}

func selectCodeLexer(code, language, filename string) chroma.Lexer {
	lang := sanitizeLanguage(language)
	if lang != "" {
		if lexer := lexers.Get(lang); lexer != nil {
			return lexer
		}
		if lexer := lexers.Match(lang); lexer != nil {
			return lexer
		}
	}

	if filename != "" {
		if lexer := lexers.Match(filename); lexer != nil {
			return lexer
		}
		if lexer := lexers.Match(filepath.Base(filename)); lexer != nil {
			return lexer
		}
	}

	if lexer := lexers.Analyse(code); lexer != nil {
		return lexer
	}

	return nil
}

func sanitizeLanguage(language string) string {
	lang := strings.TrimSpace(language)
	lang = strings.TrimPrefix(lang, ".")

	for i, r := range lang {
		switch r {
		case ' ', '\t', '{', '}', ',', ';':
			lang = lang[:i]
			return strings.TrimSpace(lang)
		}
	}

	return strings.TrimSpace(lang)
}
