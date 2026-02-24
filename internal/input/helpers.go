package input

import (
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/francescoalemanno/raijin-mono/internal/fsutil"
	"github.com/francescoalemanno/raijin-mono/internal/skills"
)

const (
	MaxImageSize = 10 * 1024 * 1024
	MaxFileSize  = 5 * 1024 * 1024
)

var (
	// Match @path at a word boundary (not after another @, as in diff hunks @@).
	// Accept quoted paths or unquoted paths that look like files (contain . or /).
	attachmentMentionRe = regexp.MustCompile(`(?:^|[^@])@("(?:[^"\\]|\\.)+"|'(?:[^'\\]|\\.)+'|[^\s@]+(?:\.[a-zA-Z0-9]+|/[^\s]*))`)
	// Match $skill-name at start or after whitespace.
	skillMentionRe = regexp.MustCompile(`(?:^|\s)\$([A-Za-z][A-Za-z0-9_-]*)`)
)

type promptAttachmentMention struct {
	token string // Original matched token (without leading @), including quotes.
	path  string // Normalized path (quotes removed, escapes unescaped).
}

// Attachment represents a file attachment for agent messages (image or text).
type Attachment struct {
	Data      []byte
	MediaType string
	Path      string
}

// SkillAttachment represents a skill explicitly loaded by user input.
type SkillAttachment struct {
	Name       string
	Content    string
	ScriptsDir string
}

// ImageMediaType returns the image MIME type for a known image extension, or "".
func ImageMediaType(ext string) string {
	switch strings.ToLower(ext) {
	case "png":
		return "image/png"
	case "jpg", "jpeg":
		return "image/jpeg"
	case "gif":
		return "image/gif"
	case "webp":
		return "image/webp"
	default:
		return ""
	}
}

// sniffTextMediaType returns "text/plain" if the file appears to be text, or "".
func sniffTextMediaType(path string) string {
	if fsutil.IsTextFile(path) {
		return "text/plain"
	}
	return ""
}

// ExtractPromptMentions returns normalized file and known-skill mentions present in the prompt.
// It uses the same detection logic as ParseAndLoadResources / ParseAndLoadSkills.
func ExtractPromptMentions(input string) (files []string, skills []string) {
	fileMentions := extractAttachmentMentions(input)
	files = make([]string, 0, len(fileMentions))
	seen := make(map[string]struct{}, len(fileMentions))
	for _, mention := range fileMentions {
		path, mediaType, info, ok := resolveAttachmentFile(mention.path)
		if !ok {
			continue
		}
		if info.Size() > maxAttachmentSize(mediaType) {
			continue
		}
		if !markSeen(seen, path) {
			continue
		}
		files = append(files, path)
	}
	return files, extractKnownSkillMentions(input)
}

// ParseAndLoadResources extracts @path references and known $skill references.
// Unknown $tokens are preserved as plain text.
func ParseAndLoadResources(input string) (string, []Attachment, []SkillAttachment, error) {
	matches := extractAttachmentMentions(input)
	var attachments []Attachment
	var errs []string
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		pathStr := match.path
		path, mediaType, info, ok := resolveAttachmentFile(pathStr)
		if !ok {
			continue
		}
		if !markSeen(seen, path) {
			continue
		}

		maxSize := maxAttachmentSize(mediaType)
		if info.Size() > maxSize {
			errs = append(errs, fmt.Sprintf("file too large: %s", path))
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			errs = append(errs, fmt.Sprintf("failed to read %s: %s", path, err))
			continue
		}
		attachments = append(attachments, Attachment{Data: data, MediaType: mediaType, Path: pathStr})
	}
	if len(errs) > 0 {
		return "", nil, nil, errors.New(strings.Join(errs, "\n"))
	}

	skillAttachments, err := ParseAndLoadSkills(input)
	if err != nil {
		return "", nil, nil, err
	}

	return strings.TrimSpace(input), attachments, skillAttachments, nil
}

// ParseAndLoadSkills extracts known $skill references and renders them as skill attachments.
func ParseAndLoadSkills(input string) ([]SkillAttachment, error) {
	fullPrompt := strings.TrimSpace(input)
	if fullPrompt == "" {
		return nil, nil
	}

	names := extractKnownSkillMentions(fullPrompt)
	if len(names) == 0 {
		return nil, nil
	}

	loaded := make([]SkillAttachment, 0, len(names))
	for _, name := range names {
		rendered, skill, err := skills.RenderSkillAttachment(name, fullPrompt)
		if err != nil {
			return nil, fmt.Errorf("failed to load skill %q: %w", name, err)
		}
		loaded = append(loaded, SkillAttachment{
			Name:       skill.Name,
			Content:    rendered,
			ScriptsDir: skill.ScriptsDir,
		})
	}
	return loaded, nil
}

func extractAttachmentMentions(input string) []promptAttachmentMention {
	matches := attachmentMentionRe.FindAllStringSubmatch(input, -1)
	mentions := make([]promptAttachmentMention, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		mentions = append(mentions, promptAttachmentMention{
			token: match[1],
			path:  normalizePath(match[1]),
		})
	}
	return mentions
}

func extractSkillMentions(input string) []string {
	matches := skillMentionRe.FindAllStringSubmatch(input, -1)
	mentions := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		mentions = append(mentions, match[1])
	}
	return mentions
}

func extractKnownSkillMentions(input string) []string {
	mentions := extractSkillMentions(input)
	seen := make(map[string]struct{}, len(mentions))
	known := make([]string, 0, len(mentions))
	for _, mention := range mentions {
		name := strings.ToLower(mention)
		if !markSeen(seen, name) {
			continue
		}
		if _, ok := skills.GetSkill(name); !ok {
			continue
		}
		known = append(known, name)
	}
	return known
}

func markSeen[K comparable](seen map[K]struct{}, key K) bool {
	if _, exists := seen[key]; exists {
		return false
	}
	seen[key] = struct{}{}
	return true
}

func resolveAttachmentFile(pathStr string) (path string, mediaType string, info os.FileInfo, ok bool) {
	path = fsutil.ExpandPath(pathStr)
	if !filepath.IsAbs(path) {
		cwd, _ := os.Getwd()
		path = filepath.Join(cwd, path)
	}
	path = filepath.Clean(path)

	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", "", nil, false
	}
	if canonicalPath, err := filepath.EvalSymlinks(path); err == nil {
		path = filepath.Clean(canonicalPath)
	}

	ext := strings.TrimPrefix(filepath.Ext(path), ".")

	// Image types take priority.
	if mt := ImageMediaType(ext); mt != "" {
		return path, mt, info, true
	}

	// Use the stdlib mime database for registered text/* types.
	if mt := mime.TypeByExtension("." + strings.ToLower(ext)); mt != "" && strings.HasPrefix(mt, "text/") {
		return path, mt, info, true
	}

	// Well-known extensionless text files.
	switch strings.ToLower(filepath.Base(path)) {
	case "makefile", "dockerfile", "vagrantfile", "gemfile",
		"rakefile", "procfile", "brewfile":
		return path, "text/plain", info, true
	}

	// Fall back to content sniffing.
	if mt := sniffTextMediaType(path); mt != "" {
		return path, mt, info, true
	}

	return "", "", nil, false
}

func maxAttachmentSize(mediaType string) int64 {
	if strings.HasPrefix(mediaType, "image/") {
		return int64(MaxImageSize)
	}
	return int64(MaxFileSize)
}

func normalizePath(pathStr string) string {
	if len(pathStr) >= 2 {
		if strings.HasPrefix(pathStr, "\"") && strings.HasSuffix(pathStr, "\"") {
			trimmed := strings.TrimSuffix(strings.TrimPrefix(pathStr, "\""), "\"")
			return strings.ReplaceAll(trimmed, `\"`, `"`)
		}
		if strings.HasPrefix(pathStr, "'") && strings.HasSuffix(pathStr, "'") {
			trimmed := strings.TrimSuffix(strings.TrimPrefix(pathStr, "'"), "'")
			return strings.ReplaceAll(trimmed, `\'`, `'`)
		}
	}
	return pathStr
}
