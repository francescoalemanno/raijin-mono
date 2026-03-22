package input

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAndLoadResourcesAttachments(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a temporary test image
	testImg := filepath.Join(tmpDir, "test.png")
	pngData := newTestPNG(t, color.NRGBA{R: 32, G: 96, B: 224, A: 128})
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
			wantFirstMediaType: "image/jpeg",
		},
		{
			name:               "quoted path with spaces",
			input:              `look at @"` + testImg + `"`,
			wantText:           `look at @"` + testImg + `"`,
			wantAttachCount:    1,
			wantFirstMediaType: "image/jpeg",
		},
		{
			name:               "single quoted path",
			input:              `see @'` + testImg + `'`,
			wantText:           `see @'` + testImg + `'`,
			wantAttachCount:    1,
			wantFirstMediaType: "image/jpeg",
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
			gotText, gotAttach, err := ParseAndLoadResources(tt.input)
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
				if strings.HasPrefix(tt.wantFirstMediaType, "image/") && !bytes.HasPrefix(gotAttach[0].Data, []byte{0xff, 0xd8, 0xff}) {
					t.Errorf("ParseAndLoadResources() first attachment is not JPEG data")
				}
			}
		})
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
	text, files, err := ParseAndLoadResources(input)
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

func newTestPNG(t *testing.T, c color.NRGBA) []byte {
	t.Helper()

	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 2; x++ {
			img.SetNRGBA(x, y, c)
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}
