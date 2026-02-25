package input

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseAndLoadResourcesAttachments(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a temporary test image
	testImg := filepath.Join(tmpDir, "test.png")
	pngData := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
		0x42, 0x60, 0x82,
	}
	if err := os.WriteFile(testImg, pngData, 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a temporary text file
	testTxt := filepath.Join(tmpDir, "hello.go")
	if err := os.WriteFile(testTxt, []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Create a Makefile (extensionless text file)
	testMakefile := filepath.Join(tmpDir, "Makefile")
	if err := os.WriteFile(testMakefile, []byte("all:\n\techo hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name               string
		input              string
		wantText           string
		wantAttachCount    int
		wantFirstMediaType string
		wantErr            bool
	}{
		// @ mentions are preserved in text while files are attached.
		{
			name:               "absolute path to existing image",
			input:              "check this @" + testImg,
			wantText:           "check this @" + testImg,
			wantAttachCount:    1,
			wantFirstMediaType: "image/png",
		},
		{
			name:               "quoted path with spaces",
			input:              `look at @"` + testImg + `"`,
			wantText:           `look at @"` + testImg + `"`,
			wantAttachCount:    1,
			wantFirstMediaType: "image/png",
		},
		{
			name:               "single quoted path",
			input:              `see @'` + testImg + `'`,
			wantText:           `see @'` + testImg + `'`,
			wantAttachCount:    1,
			wantFirstMediaType: "image/png",
		},

		// Text file attachments
		{
			name:               "go source file",
			input:              "review @" + testTxt,
			wantText:           "review @" + testTxt,
			wantAttachCount:    1,
			wantFirstMediaType: "text/plain",
		},
		{
			name:               "makefile (extensionless)",
			input:              "check @" + testMakefile,
			wantText:           "check @" + testMakefile,
			wantAttachCount:    1,
			wantFirstMediaType: "text/plain",
		},
		{
			name:            "image and text together",
			input:           "look @" + testImg + " and @" + testTxt,
			wantText:        "look @" + testImg + " and @" + testTxt,
			wantAttachCount: 2,
		},

		// Non-matching patterns
		{
			name:            "diff hunk header @@",
			input:           "@@ -1,5 +1,5 @@",
			wantText:        "@@ -1,5 +1,5 @@",
			wantAttachCount: 0,
		},
		{
			name:            "email address",
			input:           "contact user@example.com for help",
			wantText:        "contact user@example.com for help",
			wantAttachCount: 0,
		},
		{
			name:            "twitter handle",
			input:           "follow @username on twitter",
			wantText:        "follow @username on twitter",
			wantAttachCount: 0,
		},
		{
			name:            "at mention without extension",
			input:           "hey @john what do you think",
			wantText:        "hey @john what do you think",
			wantAttachCount: 0,
		},

		// Non-existent files
		{
			name:            "non-existent image file",
			input:           "check @/nonexistent/image.png please",
			wantText:        "check @/nonexistent/image.png please",
			wantAttachCount: 0,
		},

		// Mixed content
		{
			name:            "real image mixed with diff hunks",
			input:           "@@ -1,2 +1,2 @@\ncheck @" + testImg + " here\n@@ -5,3 +5,3 @@",
			wantText:        "@@ -1,2 +1,2 @@\ncheck @" + testImg + " here\n@@ -5,3 +5,3 @@",
			wantAttachCount: 1,
		},

		// Edge cases
		{
			name:            "empty input",
			input:           "",
			wantText:        "",
			wantAttachCount: 0,
		},
		{
			name:            "just whitespace",
			input:           "   ",
			wantText:        "",
			wantAttachCount: 0,
		},
		{
			name:            "no @ symbols",
			input:           "just regular text here",
			wantText:        "just regular text here",
			wantAttachCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotText, gotAttach, loadedSkills, err := ParseAndLoadResources(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseAndLoadResources() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if gotText != tt.wantText {
				t.Errorf("ParseAndLoadResources() text = %q, want %q", gotText, tt.wantText)
			}
			if len(gotAttach) != tt.wantAttachCount {
				t.Errorf("ParseAndLoadResources() attachment count = %d, want %d", len(gotAttach), tt.wantAttachCount)
			}
			if tt.wantFirstMediaType != "" && len(gotAttach) > 0 {
				if gotAttach[0].MediaType != tt.wantFirstMediaType {
					t.Errorf("ParseAndLoadResources() first media type = %q, want %q", gotAttach[0].MediaType, tt.wantFirstMediaType)
				}
			}
			if len(loadedSkills) != 0 {
				t.Errorf("ParseAndLoadResources() skills = %d, want 0", len(loadedSkills))
			}
		})
	}
}

func TestParseAndLoadResourcesSkills(t *testing.T) {
	t.Parallel()

	text, files, loadedSkills, err := ParseAndLoadResources("$commit these changes")
	if err != nil {
		t.Fatalf("ParseAndLoadResources() unexpected error: %v", err)
	}
	if text != "$commit these changes" {
		t.Fatalf("ParseAndLoadResources() text = %q, want unchanged", text)
	}
	if len(files) != 0 {
		t.Fatalf("ParseAndLoadResources() files = %d, want 0", len(files))
	}
	if len(loadedSkills) != 1 {
		t.Fatalf("ParseAndLoadResources() skills = %d, want 1", len(loadedSkills))
	}
	if loadedSkills[0].Name != "commit" {
		t.Fatalf("loaded skill name = %q, want commit", loadedSkills[0].Name)
	}
	if loadedSkills[0].Content == "" {
		t.Fatalf("loaded skill content should not be empty")
	}
}

func TestParseAndLoadResourcesUnknownSkillIgnored(t *testing.T) {
	t.Parallel()

	text, files, loadedSkills, err := ParseAndLoadResources("$phpVar should stay")
	if err != nil {
		t.Fatalf("ParseAndLoadResources() unexpected error: %v", err)
	}
	if text != "$phpVar should stay" {
		t.Fatalf("ParseAndLoadResources() text = %q, want unchanged", text)
	}
	if len(files) != 0 {
		t.Fatalf("ParseAndLoadResources() files = %d, want 0", len(files))
	}
	if len(loadedSkills) != 0 {
		t.Fatalf("ParseAndLoadResources() skills = %d, want 0", len(loadedSkills))
	}
}

func TestParseAndLoadResourcesDedupSkills(t *testing.T) {
	t.Parallel()

	_, _, loadedSkills, err := ParseAndLoadResources("$commit now and $commit again")
	if err != nil {
		t.Fatalf("ParseAndLoadResources() unexpected error: %v", err)
	}
	if len(loadedSkills) != 1 {
		t.Fatalf("ParseAndLoadResources() skills = %d, want 1", len(loadedSkills))
	}
}

func TestParseAndLoadResourcesDedupAttachments(t *testing.T) {
	tmpDir := t.TempDir()
	testTxt := filepath.Join(tmpDir, "dup.txt")
	if err := os.WriteFile(testTxt, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	input := "review @dup.txt and @" + testTxt + " and @dup.txt again"
	text, files, loadedSkills, err := ParseAndLoadResources(input)
	if err != nil {
		t.Fatalf("ParseAndLoadResources() unexpected error: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("ParseAndLoadResources() attachment count = %d, want 1", len(files))
	}
	if files[0].Path != "dup.txt" {
		t.Fatalf("ParseAndLoadResources() first attachment path = %q, want %q", files[0].Path, "dup.txt")
	}
	if text != input {
		t.Fatalf("ParseAndLoadResources() text = %q, want %q", text, input)
	}
	if len(loadedSkills) != 0 {
		t.Fatalf("ParseAndLoadResources() skills = %d, want 0", len(loadedSkills))
	}
}

func TestParseAndLoadSkills(t *testing.T) {
	t.Parallel()

	loadedSkills, err := ParseAndLoadSkills("$commit now and $commit again and $phpVar")
	if err != nil {
		t.Fatalf("ParseAndLoadSkills() unexpected error: %v", err)
	}
	if len(loadedSkills) != 1 {
		t.Fatalf("ParseAndLoadSkills() skills = %d, want 1", len(loadedSkills))
	}
	if loadedSkills[0].Name != "commit" {
		t.Fatalf("loaded skill name = %q, want commit", loadedSkills[0].Name)
	}
	if loadedSkills[0].Content == "" {
		t.Fatalf("loaded skill content should not be empty")
	}
}

func TestParseAndLoadSkillsCaseInsensitiveDedup(t *testing.T) {
	t.Parallel()

	loadedSkills, err := ParseAndLoadSkills("$commit now and $COMMIT again")
	if err != nil {
		t.Fatalf("ParseAndLoadSkills() unexpected error: %v", err)
	}
	if len(loadedSkills) != 1 {
		t.Fatalf("ParseAndLoadSkills() skills = %d, want 1", len(loadedSkills))
	}
	if loadedSkills[0].Name != "commit" {
		t.Fatalf("loaded skill name = %q, want commit", loadedSkills[0].Name)
	}
}

func TestExtractPromptMentions(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmpDir, "docs"), 0o755); err != nil {
		t.Fatalf("failed to create docs dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "pkg"), 0o755); err != nil {
		t.Fatalf("failed to create pkg dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "README.md"), []byte("# test\n"), 0o644); err != nil {
		t.Fatalf("failed to create README.md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "docs", "my file.txt"), []byte("notes\n"), 0o644); err != nil {
		t.Fatalf("failed to create docs/my file.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("failed to create main.go: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "pkg", "file.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("failed to create pkg/file.txt: %v", err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working dir: %v", err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(wd)
	})

	// resolve tmpDir through symlinks to match what resolveAttachmentFile returns
	resolvedTmpDir, err := filepath.EvalSymlinks(tmpDir)
	if err != nil {
		t.Fatalf("failed to eval symlinks for tmpDir: %v", err)
	}

	abs := func(rel string) string { return filepath.Join(resolvedTmpDir, rel) }

	tests := []struct {
		name       string
		input      string
		wantFiles  []string
		wantSkills []string
	}{
		{
			name:       "files and known skill",
			input:      `review @README.md and @"docs/my file.txt" with $commit and $phpVar`,
			wantFiles:  []string{abs("README.md"), abs("docs/my file.txt")},
			wantSkills: []string{"commit"},
		},
		{
			name:       "ignore diff hunks and handle deduped skills",
			input:      "@@ -1,2 +1,2 @@\n$commit then $COMMIT",
			wantFiles:  []string{},
			wantSkills: []string{"commit"},
		},
		{
			name:       "only complete file-looking mentions",
			input:      "hello @john and @pkg/file.txt and @main.go and @missing.go",
			wantFiles:  []string{abs("pkg/file.txt"), abs("main.go")},
			wantSkills: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotFiles, gotSkills := ExtractPromptMentions(tt.input)
			if !reflect.DeepEqual(gotFiles, tt.wantFiles) {
				t.Fatalf("ExtractPromptMentions() files = %#v, want %#v", gotFiles, tt.wantFiles)
			}
			if !reflect.DeepEqual(gotSkills, tt.wantSkills) {
				t.Fatalf("ExtractPromptMentions() skills = %#v, want %#v", gotSkills, tt.wantSkills)
			}
		})
	}
}

func TestExtractPromptMentionsDedupFiles(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pngData := []byte{
		0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41,
		0x54, 0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00,
		0x05, 0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00,
		0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae,
		0x42, 0x60, 0x82,
	}
	imgPath := filepath.Join(tmpDir, "i.png")
	if err := os.WriteFile(imgPath, pngData, 0o644); err != nil {
		t.Fatal(err)
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmpDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })

	resolvedTmpDir, err := filepath.EvalSymlinks(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	files, skills := ExtractPromptMentions("check @a.txt @" + filepath.Join(tmpDir, "a.txt") + " @i.png @" + imgPath + " $commit")
	wantFiles := []string{filepath.Join(resolvedTmpDir, "a.txt"), filepath.Join(resolvedTmpDir, "i.png")}
	if !reflect.DeepEqual(files, wantFiles) {
		t.Fatalf("ExtractPromptMentions() files = %#v, want %#v", files, wantFiles)
	}
	wantSkills := []string{"commit"}
	if !reflect.DeepEqual(skills, wantSkills) {
		t.Fatalf("ExtractPromptMentions() skills = %#v, want %#v", skills, wantSkills)
	}
}

func TestResolveAttachmentFileTildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("Cannot get home directory:", err)
	}

	// Create a test file in home directory
	testFile := filepath.Join(home, "test_tilde_attachment.txt")
	if err := os.WriteFile(testFile, []byte("test content"), 0o644); err != nil {
		t.Skip("Cannot create test file:", err)
	}
	defer os.Remove(testFile)

	// Test unquoted tilde path
	path, mediaType, info, ok := resolveAttachmentFile("~/test_tilde_attachment.txt")
	if !ok {
		t.Fatal("Failed to resolve unquoted tilde path")
	}
	if !strings.HasPrefix(path, home) {
		t.Errorf("Expected path to start with home directory %q, got %q", home, path)
	}
	if mediaType != "text/plain" && !strings.HasPrefix(mediaType, "text/plain") {
		t.Errorf("Expected text/plain media type, got %q", mediaType)
	}
	if info == nil {
		t.Error("Expected non-nil file info")
	}

	// Test quoted tilde path (quotes are stripped by normalizePath before resolveAttachmentFile)
	path, _, _, ok = resolveAttachmentFile("~/test_tilde_attachment.txt")
	if !ok {
		t.Fatal("Failed to resolve quoted tilde path")
	}
	if !strings.HasPrefix(path, home) {
		t.Errorf("Expected path to start with home directory %q, got %q", home, path)
	}

	// Verify the file is actually readable
	data, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("Failed to read resolved file: %v", err)
	}
	if string(data) != "test content" {
		t.Errorf("File content mismatch: got %q, want %q", string(data), "test content")
	}
}
