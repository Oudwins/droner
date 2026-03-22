package tui

import (
	"testing"

	"github.com/Oudwins/droner/pkgs/droner/internals/messages"
	tea "github.com/charmbracelet/bubbletea"
)

func TestBuildSessionCreateRequestPreservesMultilinePrompt(t *testing.T) {
	prompt := "first line\n\nsecond line\n"
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
	if parts[0].Text != prompt {
		t.Fatalf("expected prompt %q, got %q", prompt, parts[0].Text)
	}
}

func TestBuildSessionCreateRequestOmitsAgentConfigForEmptyPrompt(t *testing.T) {
	request := buildSessionCreateRequest("/tmp/repo", "  \n\t  ")

	if request.AgentConfig != nil {
		t.Fatal("expected agent config to be omitted for empty prompt")
	}
	if request.SessionID != "" {
		t.Fatalf("expected session ID to be omitted, got %q", request.SessionID)
	}
}

func TestSessionComposerEnterSubmitsRawValue(t *testing.T) {
	model := newSessionComposerModel()
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
	if prompt != "first line\nsecond line\n" {
		t.Fatalf("expected raw prompt to be preserved, got %q", prompt)
	}
}

func TestSessionComposerAltEnterInsertsNewline(t *testing.T) {
	model := newSessionComposerModel()
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
	model := newSessionComposerModel()
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
	model := newSessionComposerModel()
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
	model := newSessionComposerModel()
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
