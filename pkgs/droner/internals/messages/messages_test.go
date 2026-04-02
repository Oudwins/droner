package messages

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToRawText(t *testing.T) {
	t.Parallel()

	if got := ToRawText(nil); got != "" {
		t.Fatalf("ToRawText(nil) = %q, want empty", got)
	}

	m := &Message{
		Role: MessageRoleUser,
		Parts: []MessagePart{
			NewTextPart("hello"),
			NewFilePart("pkgs/droner/tui/tui.go"),
			{Type: PartType("image"), Text: "ignored"},
			NewTextPart(""),
			NewTextPart("world"),
		},
	}

	if got, want := ToRawText(m), "hello\nworld"; got != want {
		t.Fatalf("ToRawText(m) = %q, want %q", got, want)
	}
}

func TestNewFilePartUsesCanonicalPathAndFilename(t *testing.T) {
	t.Parallel()

	part := NewFilePart("pkgs/droner/tui/../tui/tui.go")

	if part.Type != PartTypeFile {
		t.Fatalf("Type = %q, want %q", part.Type, PartTypeFile)
	}
	if part.File == nil {
		t.Fatal("expected nested file payload")
	}
	if part.File.URL != nil {
		t.Fatalf("URL = %#v, want nil", part.File.URL)
	}
	if part.File.Source == nil || part.File.Source.Path != "pkgs/droner/tui/tui.go" {
		t.Fatalf("Source.Path = %#v, want pkgs/droner/tui/tui.go", part.File.Source)
	}
	if part.File.Filename != "tui.go" {
		t.Fatalf("Filename = %q, want tui.go", part.File.Filename)
	}
}

func TestCloneMessageCopiesPartsSlice(t *testing.T) {
	t.Parallel()

	original := &Message{
		Role:  MessageRoleUser,
		Parts: []MessagePart{NewTextPart("alpha"), NewFilePart("pkgs/droner/tui/tui.go")},
	}
	clone := CloneMessage(original)
	clone.Parts[0].Text = "beta"
	clone.Parts[1].File.Source.Path = "other.go"

	if original.Parts[0].Text != "alpha" {
		t.Fatalf("original text mutated to %q", original.Parts[0].Text)
	}
	if original.Parts[1].File.Source.Path != "pkgs/droner/tui/tui.go" {
		t.Fatalf("original file path mutated to %q", original.Parts[1].File.Source.Path)
	}
}

func TestNewDataURLFilePartIsValid(t *testing.T) {
	t.Parallel()

	part := NewDataURLFilePart("image/png", "pasted-image-1.png", "data:image/png;base64,ZmFrZQ==")

	if !part.isValid() {
		t.Fatalf("expected inline data url part to be valid: %#v", part)
	}
	if part.File == nil || part.File.Source != nil {
		t.Fatalf("expected inline file part without source, got %#v", part.File)
	}
}

func TestFilePartRejectsMissingURLAndSource(t *testing.T) {
	t.Parallel()

	part := MessagePart{
		Type: PartTypeFile,
		File: &FilePartData{
			Mime:     "image/png",
			Filename: "broken.png",
		},
	}

	if part.isValid() {
		t.Fatalf("expected invalid file part, got %#v", part)
	}
}

func TestFilePartRejectsMixedInlineURLAndSource(t *testing.T) {
	t.Parallel()

	part := NewDataURLFilePart("image/png", "pasted-image-1.png", "data:image/png;base64,ZmFrZQ==")
	part.File.Source = NewFilePart("README.md").File.Source

	if part.isValid() {
		t.Fatalf("expected mixed file part to be invalid, got %#v", part)
	}
}

func TestCloneMessageCopiesInlineFileParts(t *testing.T) {
	t.Parallel()

	original := &Message{
		Role: MessageRoleUser,
		Parts: []MessagePart{
			NewDataURLFilePart("image/png", "pasted-image-1.png", "data:image/png;base64,ZmFrZQ=="),
		},
	}
	clone := CloneMessage(original)
	*clone.Parts[0].File.URL = "data:image/png;base64,Y2hhbmdlZA=="
	clone.Parts[0].File.Filename = "changed.png"

	if got := *original.Parts[0].File.URL; got != "data:image/png;base64,ZmFrZQ==" {
		t.Fatalf("original url mutated to %q", got)
	}
	if got := original.Parts[0].File.Filename; got != "pasted-image-1.png" {
		t.Fatalf("original filename mutated to %q", got)
	}
}

func TestCommandInvocationText(t *testing.T) {
	t.Parallel()

	command := &CommandInvocation{Name: "review", Arguments: "src/app.ts"}
	if got := command.InvocationText(); got != "/review src/app.ts" {
		t.Fatalf("InvocationText() = %q, want %q", got, "/review src/app.ts")
	}

	command.Arguments = ""
	if got := command.InvocationText(); got != "/review" {
		t.Fatalf("InvocationText() without arguments = %q, want %q", got, "/review")
	}
}

func TestCloneCommandCopiesPartsSlice(t *testing.T) {
	t.Parallel()

	original := &CommandInvocation{
		Name:      "review",
		Arguments: "README.md",
		Parts: []MessagePart{
			NewFilePart("README.md"),
			NewDataURLFilePart("image/png", "pasted-image-1.png", "data:image/png;base64,ZmFrZQ=="),
		},
	}
	clone := CloneCommand(original)
	clone.Parts[0].File.Source.Path = "other.md"
	*clone.Parts[1].File.URL = "data:image/png;base64,Y2hhbmdlZA=="

	if original.Parts[0].File.Source.Path != "README.md" {
		t.Fatalf("original file path mutated to %q", original.Parts[0].File.Source.Path)
	}
	if got := *original.Parts[1].File.URL; got != "data:image/png;base64,ZmFrZQ==" {
		t.Fatalf("original inline file url mutated to %q", got)
	}
}

func TestNewFilePartMarshalsNullURL(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(NewFilePart("pkgs/droner/tui/tui.go"))
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if !strings.Contains(string(encoded), `"url":null`) {
		t.Fatalf("expected marshaled file part to contain null url, got %s", encoded)
	}
}

func TestMessagePartsForLogExpandsNestedFileText(t *testing.T) {
	t.Parallel()

	parts := messagePartsForLog([]MessagePart{NewFilePart("README.md")})
	fileItem, ok := parts[0]["file"].(map[string]any)
	if !ok {
		t.Fatalf("file item = %#v, want map", parts[0]["file"])
	}
	sourceItem, ok := fileItem["source"].(map[string]any)
	if !ok {
		t.Fatalf("source item = %#v, want map", fileItem["source"])
	}
	textItem, ok := sourceItem["text"].(map[string]any)
	if !ok {
		t.Fatalf("text item = %#v, want map", sourceItem["text"])
	}
	if textItem["start"] != int64(0) || textItem["end"] != int64(0) || textItem["value"] != "" {
		t.Fatalf("unexpected text item: %#v", textItem)
	}
	if _, exists := fileItem["url"]; !exists {
		t.Fatalf("expected url key in logged file item: %#v", fileItem)
	}
}

func TestMessagePartsForLogRedactsInlineDataURLs(t *testing.T) {
	t.Parallel()

	parts := messagePartsForLog([]MessagePart{NewDataURLFilePart("image/png", "pasted-image-1.png", "data:image/png;base64,ZmFrZUJhc2U2NA==")})
	fileItem, ok := parts[0]["file"].(map[string]any)
	if !ok {
		t.Fatalf("file item = %#v, want map", parts[0]["file"])
	}
	if got := fileItem["url"]; got != "data:image/png;base64,<redacted>" {
		t.Fatalf("url = %#v, want redacted data url", got)
	}
	if got := fileItem["urlTruncated"]; got != true {
		t.Fatalf("urlTruncated = %#v, want true", got)
	}
	if got := fileItem["urlLength"]; got != len("data:image/png;base64,ZmFrZUJhc2U2NA==") {
		t.Fatalf("urlLength = %#v, want %d", got, len("data:image/png;base64,ZmFrZUJhc2U2NA=="))
	}
	if got := fileItem["mime"]; got != "image/png" {
		t.Fatalf("mime = %#v, want image/png", got)
	}
	if got := fileItem["filename"]; got != "pasted-image-1.png" {
		t.Fatalf("filename = %#v, want pasted-image-1.png", got)
	}
}

func TestMessagePartsForLogLeavesNonDataURLsUnchanged(t *testing.T) {
	t.Parallel()

	url := "file:///tmp/example.txt"
	parts := messagePartsForLog([]MessagePart{{
		Type: PartTypeFile,
		File: &FilePartData{
			URL:      &url,
			Mime:     "text/plain",
			Filename: "example.txt",
		},
	}})
	fileItem, ok := parts[0]["file"].(map[string]any)
	if !ok {
		t.Fatalf("file item = %#v, want map", parts[0]["file"])
	}
	if got := fileItem["url"]; got != url {
		t.Fatalf("url = %#v, want %q", got, url)
	}
	if _, exists := fileItem["urlTruncated"]; exists {
		t.Fatalf("expected non-data url to omit truncation metadata: %#v", fileItem)
	}
	if _, exists := fileItem["urlLength"]; exists {
		t.Fatalf("expected non-data url to omit length metadata: %#v", fileItem)
	}
}

func TestIsValidRepoRelativePath(t *testing.T) {
	t.Parallel()

	if !isValidRepoRelativePath("pkgs/droner/tui/tui.go") {
		t.Fatal("expected canonical relative path to be valid")
	}
	for _, path := range []string{"", ".", "..", "../outside.go", "/tmp/absolute.go", "pkgs/droner/../tui.go"} {
		if isValidRepoRelativePath(path) {
			t.Fatalf("expected %q to be invalid", path)
		}
	}
}
