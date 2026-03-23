package tui

import (
	"strings"
	"testing"

	"github.com/Oudwins/droner/pkgs/droner/internals/messages"
	tea "github.com/charmbracelet/bubbletea"
)

func TestBuildSessionCreateRequestPreservesMultilinePrompt(t *testing.T) {
	prompt := &messages.Message{
		Role: messages.MessageRoleUser,
		Parts: []messages.MessagePart{
			messages.NewTextPart("first line\n\nsecond line\n"),
		},
	}
	request := buildSessionCreateRequest("/tmp/repo", prompt)

	if request.Path != "/tmp/repo" {
		t.Fatalf("expected path to be preserved, got %q", request.Path)
	}
	if request.SessionID != "" {
		t.Fatalf("expected session ID to be omitted, got %q", request.SessionID)
	}
	if request.AgentConfig == nil || request.AgentConfig.Message == nil {
		t.Fatal("expected agent message to be included")
	}
	if request.AgentConfig.Message.Role != messages.MessageRoleUser {
		t.Fatalf("expected user role, got %q", request.AgentConfig.Message.Role)
	}
	parts := request.AgentConfig.Message.Parts
	if len(parts) != 1 {
		t.Fatalf("expected one message part, got %d", len(parts))
	}
	if parts[0].Text != "first line\n\nsecond line\n" {
		t.Fatalf("expected prompt %q, got %q", "first line\n\nsecond line\n", parts[0].Text)
	}
}

func TestBuildSessionCreateRequestPreservesFileParts(t *testing.T) {
	prompt := &messages.Message{
		Role: messages.MessageRoleUser,
		Parts: []messages.MessagePart{
			messages.NewTextPart("inspect "),
			messages.NewFilePart("pkgs/droner/tui/tui.go"),
		},
	}
	request := buildSessionCreateRequest("/tmp/repo", prompt)

	if request.AgentConfig == nil || request.AgentConfig.Message == nil {
		t.Fatal("expected agent message to be included")
	}
	parts := request.AgentConfig.Message.Parts
	if len(parts) != 2 {
		t.Fatalf("expected two parts, got %d", len(parts))
	}
	if parts[1].Type != messages.PartTypeFile || parts[1].File == nil || parts[1].File.Source == nil || parts[1].File.Source.Path != "pkgs/droner/tui/tui.go" {
		t.Fatalf("unexpected file part: %#v", parts[1])
	}
	if parts[1].File.URL != nil {
		t.Fatalf("expected file url to be nil in TUI payload, got %#v", parts[1].File.URL)
	}
}

func TestBuildSessionCreateRequestPreservesInlineImageParts(t *testing.T) {
	prompt := &messages.Message{
		Role: messages.MessageRoleUser,
		Parts: []messages.MessagePart{
			messages.NewDataURLFilePart("image/png", "pasted-image-1.png", "data:image/png;base64,ZmFrZQ=="),
		},
	}
	request := buildSessionCreateRequest("/tmp/repo", prompt)

	if request.AgentConfig == nil || request.AgentConfig.Message == nil {
		t.Fatal("expected agent message to be included")
	}
	parts := request.AgentConfig.Message.Parts
	if len(parts) != 1 {
		t.Fatalf("expected one part, got %d", len(parts))
	}
	if parts[0].File == nil || parts[0].File.URL == nil || *parts[0].File.URL != "data:image/png;base64,ZmFrZQ==" {
		t.Fatalf("unexpected inline image part: %#v", parts[0])
	}
	if parts[0].File.Source != nil {
		t.Fatalf("expected inline image source to stay nil, got %#v", parts[0].File.Source)
	}
}

func TestBuildSessionCreateRequestOmitsAgentConfigForEmptyPrompt(t *testing.T) {
	request := buildSessionCreateRequest("/tmp/repo", &messages.Message{
		Role:  messages.MessageRoleUser,
		Parts: []messages.MessagePart{messages.NewTextPart("  \n\t  ")},
	})

	if request.AgentConfig != nil {
		t.Fatal("expected agent config to be omitted for empty prompt")
	}
	if request.SessionID != "" {
		t.Fatalf("expected session ID to be omitted, got %q", request.SessionID)
	}
}

func TestSessionComposerEnterSubmitsStructuredMessage(t *testing.T) {
	model := newSessionComposerModel("", nil)
	model.input.SetValue("first line\nsecond line\n")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	finalModel := updated.(sessionComposerModel)

	if !finalModel.submitted {
		t.Fatal("expected composer to submit")
	}
	if finalModel.cancelled {
		t.Fatal("did not expect composer to cancel")
	}
	prompt, submitted, err := extractComposerResult(finalModel)
	if err != nil {
		t.Fatalf("unexpected error extracting result: %v", err)
	}
	if !submitted {
		t.Fatal("expected extracted result to be submitted")
	}
	if prompt == nil {
		t.Fatal("expected structured prompt message")
	}
	if prompt.Role != messages.MessageRoleUser {
		t.Fatalf("expected user role, got %q", prompt.Role)
	}
	if len(prompt.Parts) != 1 {
		t.Fatalf("expected one prompt part, got %d", len(prompt.Parts))
	}
	if prompt.Parts[0] != messages.NewTextPart("first line\nsecond line\n") {
		t.Fatalf("expected raw prompt to be preserved, got %#v", prompt.Parts[0])
	}
}

func TestSessionComposerAltEnterInsertsNewline(t *testing.T) {
	model := newSessionComposerModel("", nil)
	model.input.SetValue("alpha")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	finalModel := updated.(sessionComposerModel)

	if finalModel.submitted {
		t.Fatal("did not expect alt+enter to submit")
	}
	if finalModel.input.Value() != "alpha\n" {
		t.Fatalf("expected newline insertion, got %q", finalModel.input.Value())
	}
}

func TestSessionComposerCtrlJFallbackInsertsNewline(t *testing.T) {
	model := newSessionComposerModel("", nil)
	model.input.SetValue("alpha")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlJ})
	finalModel := updated.(sessionComposerModel)

	if finalModel.submitted {
		t.Fatal("did not expect ctrl+j to submit")
	}
	if finalModel.input.Value() != "alpha\n" {
		t.Fatalf("expected newline insertion, got %q", finalModel.input.Value())
	}
}

func TestSessionComposerEscCancels(t *testing.T) {
	model := newSessionComposerModel("", nil)
	model.input.SetValue("alpha")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	finalModel := updated.(sessionComposerModel)

	if !finalModel.cancelled {
		t.Fatal("expected composer to cancel")
	}
	if finalModel.submitted {
		t.Fatal("did not expect cancelled composer to submit")
	}
	_, submitted, err := extractComposerResult(finalModel)
	if err != nil {
		t.Fatalf("unexpected error extracting cancelled result: %v", err)
	}
	if submitted {
		t.Fatal("expected cancelled result to be unsubmitted")
	}
}

func TestSessionComposerRejectsWhitespaceSubmit(t *testing.T) {
	model := newSessionComposerModel("", nil)
	model.input.SetValue("  \n\t")

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	finalModel := updated.(sessionComposerModel)

	if finalModel.submitted {
		t.Fatal("did not expect whitespace prompt to submit")
	}
	if finalModel.validationMessage == "" {
		t.Fatal("expected validation message for empty submit")
	}
}

func TestComposerPromptSetPlainTextPreservesExactValue(t *testing.T) {
	prompt := newComposerPrompt()
	prompt.SetPlainText("  alpha\n")

	if prompt.PlainText() != "  alpha\n" {
		t.Fatalf("PlainText() = %q, want %q", prompt.PlainText(), "  alpha\n")
	}
	if prompt.IsEmpty() {
		t.Fatal("expected non-empty prompt")
	}
	message := prompt.Message()
	if message == nil || len(message.Parts) != 1 {
		t.Fatalf("expected one message part, got %#v", message)
	}
	if message.Parts[0] != messages.NewTextPart("  alpha\n") {
		t.Fatalf("message part = %#v, want %#v", message.Parts[0], messages.NewTextPart("  alpha\n"))
	}
}

func TestComposerPromptWhitespaceOnlyIsEmpty(t *testing.T) {
	prompt := newComposerPrompt()
	prompt.SetPlainText(" \n\t ")

	if !prompt.IsEmpty() {
		t.Fatal("expected whitespace-only prompt to be empty")
	}
}

func TestComposerPromptTracksFileReferences(t *testing.T) {
	prompt := newComposerPrompt()
	prompt.SetPlainText("inspect @pkgs/droner/tui/tui.go now")
	prompt.AddFileRef(8, 31, "pkgs/droner/tui/tui.go")

	message := prompt.Message()
	if len(message.Parts) != 3 {
		t.Fatalf("expected three parts, got %#v", message.Parts)
	}
	if message.Parts[1].Type != messages.PartTypeFile {
		t.Fatalf("expected file part, got %#v", message.Parts[1])
	}
	if message.Parts[1].File == nil || message.Parts[1].File.Source == nil || message.Parts[1].File.Source.Path != "pkgs/droner/tui/tui.go" {
		t.Fatalf("file source = %#v, want pkgs/droner/tui/tui.go", message.Parts[1].File)
	}
	if message.Parts[1].File.URL != nil {
		t.Fatalf("expected nil file url, got %#v", message.Parts[1].File.URL)
	}
}

func TestComposerPromptEditingInsideFileReferenceDropsStructuredSpan(t *testing.T) {
	prompt := newComposerPrompt()
	prompt.SetPlainText("inspect @pkgs/droner/tui/tui.go now")
	prompt.AddFileRef(8, 31, "pkgs/droner/tui/tui.go")
	prompt.SyncText("inspect @pkgs/droner/tui/tuix.go now")

	message := prompt.Message()
	if len(message.Parts) != 1 {
		t.Fatalf("expected one plain-text part after edit, got %#v", message.Parts)
	}
	if message.Parts[0].Type != messages.PartTypeText {
		t.Fatalf("expected text part, got %#v", message.Parts[0])
	}
}

func TestComposerPromptPreservesMixedTextFileAndImageParts(t *testing.T) {
	prompt := newComposerPrompt()
	text := "inspect @pkgs/droner/tui/tui.go and [Image 1] now"
	prompt.SetPlainText(text)
	fileStart := strings.Index(text, "@pkgs/droner/tui/tui.go")
	imageStart := strings.Index(text, "[Image 1]")
	prompt.AddFileRef(fileStart, fileStart+len("@pkgs/droner/tui/tui.go"), "pkgs/droner/tui/tui.go")
	prompt.AddStructuredPart(imageStart, imageStart+len("[Image 1]"), "[Image 1]", messages.NewDataURLFilePart("image/png", "pasted-image-1.png", "data:image/png;base64,ZmFrZQ=="))

	message := prompt.Message()
	if len(message.Parts) != 5 {
		t.Fatalf("expected five parts, got %#v", message.Parts)
	}
	if message.Parts[1].File == nil || message.Parts[1].File.Source == nil || message.Parts[1].File.Source.Path != "pkgs/droner/tui/tui.go" {
		t.Fatalf("expected repo file part, got %#v", message.Parts[1])
	}
	if message.Parts[3].File == nil || message.Parts[3].File.URL == nil || *message.Parts[3].File.URL != "data:image/png;base64,ZmFrZQ==" {
		t.Fatalf("expected inline image part, got %#v", message.Parts[3])
	}
}

func TestComposerPromptEditingInsideImageMarkerDropsStructuredToken(t *testing.T) {
	prompt := newComposerPrompt()
	text := "inspect [Image 1] now"
	prompt.SetPlainText(text)
	imageStart := strings.Index(text, "[Image 1]")
	prompt.AddStructuredPart(imageStart, imageStart+len("[Image 1]"), "[Image 1]", messages.NewDataURLFilePart("image/png", "pasted-image-1.png", "data:image/png;base64,ZmFrZQ=="))
	prompt.SyncText("inspect [Image x] now")

	message := prompt.Message()
	if len(message.Parts) != 1 {
		t.Fatalf("expected plain-text prompt after marker edit, got %#v", message.Parts)
	}
	if message.Parts[0].Type != messages.PartTypeText {
		t.Fatalf("expected text part, got %#v", message.Parts[0])
	}
}

func TestSessionComposerTabInsertsStructuredFileReference(t *testing.T) {
	model := newSessionComposerModel("/tmp/repo", []string{"pkgs/droner/tui/tui.go", "README.md"})
	model.input.SetValue("inspect @pkgs/dr")
	model.syncPromptFromInput()
	model.refreshAutocomplete()

	if !model.autocompleteActive {
		t.Fatal("expected autocomplete to be active")
	}
	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	finalModel := updated.(sessionComposerModel)

	if got := finalModel.input.Value(); got != "inspect @pkgs/droner/tui/tui.go" {
		t.Fatalf("input value = %q", got)
	}
	message := finalModel.prompt.Message()
	if len(message.Parts) != 2 {
		t.Fatalf("expected text and file parts, got %#v", message.Parts)
	}
	if message.Parts[1].Type != messages.PartTypeFile {
		t.Fatalf("expected file part, got %#v", message.Parts[1])
	}
}

func TestSessionComposerCtrlVPastesImageMarker(t *testing.T) {
	model := newSessionComposerModel("", nil)
	model.readClipboardImage = func() (clipboardImage, bool, error) {
		return clipboardImage{Bytes: []byte("fake"), Mime: "image/png"}, true, nil
	}

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if cmd != nil {
		t.Fatalf("expected inline image paste to complete synchronously, got cmd %v", cmd)
	}
	finalModel := updated.(sessionComposerModel)

	if got := finalModel.input.Value(); got != "[Image 1]" {
		t.Fatalf("input value = %q, want [Image 1]", got)
	}
	message := finalModel.prompt.Message()
	if len(message.Parts) != 1 {
		t.Fatalf("expected one inline image part, got %#v", message.Parts)
	}
	if message.Parts[0].File == nil || message.Parts[0].File.URL == nil {
		t.Fatalf("expected inline image file payload, got %#v", message.Parts[0])
	}
	if !strings.HasPrefix(*message.Parts[0].File.URL, "data:image/png;base64,") {
		t.Fatalf("url = %q, want data url", *message.Parts[0].File.URL)
	}
	if message.Parts[0].File.Filename != "pasted-image-1.png" {
		t.Fatalf("filename = %q, want pasted-image-1.png", message.Parts[0].File.Filename)
	}
	attachmentView := finalModel.imageAttachmentView(80)
	if !strings.Contains(attachmentView, "Images:") || !strings.Contains(attachmentView, "[Image 1]") {
		t.Fatalf("attachment view = %q, want image feedback", attachmentView)
	}
}

func TestSessionComposerCtrlVFallsBackToTextPasteWhenNoImage(t *testing.T) {
	model := newSessionComposerModel("", nil)
	model.readClipboardImage = func() (clipboardImage, bool, error) {
		return clipboardImage{}, false, nil
	}
	model.pasteTextCmd = func() tea.Msg {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("pasted")}
	}

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if cmd == nil {
		t.Fatal("expected text paste fallback cmd")
	}
	updated, _ = updated.(sessionComposerModel).Update(cmd())
	finalModel := updated.(sessionComposerModel)

	if got := finalModel.input.Value(); got != "pasted" {
		t.Fatalf("input value = %q, want pasted", got)
	}
	if finalModel.validationMessage != "" {
		t.Fatalf("validation message = %q, want empty", finalModel.validationMessage)
	}
}

func TestSessionComposerCtrlVShowsMessageWhenNoImageOrTextWasPasted(t *testing.T) {
	model := newSessionComposerModel("", nil)
	model.readClipboardImage = func() (clipboardImage, bool, error) {
		return clipboardImage{}, false, nil
	}
	model.pasteTextCmd = func() tea.Msg { return nil }

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	if cmd == nil {
		t.Fatal("expected text paste fallback cmd")
	}
	updated, _ = updated.(sessionComposerModel).Update(cmd())
	finalModel := updated.(sessionComposerModel)

	if finalModel.validationMessage != "No clipboard image was detected." {
		t.Fatalf("validation message = %q", finalModel.validationMessage)
	}
}

func TestSessionComposerViewShowsInlineImageFeedback(t *testing.T) {
	model := newSessionComposerModel("", nil)
	model.ready = true
	model.width = 80
	model.height = 24
	model.input.SetWidth(60)
	model.input.SetValue("inspect [Image 1]")
	model.syncPromptFromInput()
	model.prompt.AddStructuredPart(strings.Index(model.input.Value(), "[Image 1]"), strings.Index(model.input.Value(), "[Image 1]")+len("[Image 1]"), "[Image 1]", messages.NewDataURLFilePart("image/png", "pasted-image-1.png", "data:image/png;base64,ZmFrZQ=="))

	view := model.View()
	if !strings.Contains(view, "Images:") {
		t.Fatalf("view = %q, want image feedback label", view)
	}
	if !strings.Contains(view, "[Image 1]") {
		t.Fatalf("view = %q, want image marker", view)
	}
}
