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
