package schemas

import (
	"strings"
	"testing"

	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
)

func TestSessionCreateSchemaDefaultsAndTrim(t *testing.T) {
	defaultModel := conf.GetConfig().Agent.DefaultModel
	req := SessionCreateRequest{
		Path:      "  /tmp/repo  ",
		SessionID: "  abc123  ",
		Agent: &SessionAgentConfig{
			Model:  "  ",
			Prompt: "  hello  ",
		},
	}

	if issues := SessionCreateSchema.Validate(&req); len(issues) > 0 {
		t.Fatalf("expected no issues, got %v", issues)
	}

	if req.Path != "/tmp/repo" {
		t.Fatalf("expected trimmed path, got %q", req.Path)
	}
	if req.SessionID != "abc123" {
		t.Fatalf("expected trimmed session id, got %q", req.SessionID)
	}
	if req.Agent == nil {
		t.Fatalf("expected agent to be defaulted")
	}
	if req.Agent.Model != defaultModel {
		t.Fatalf("expected default model %q, got %q", defaultModel, req.Agent.Model)
	}
	if req.Agent.Prompt != "hello" {
		t.Fatalf("expected trimmed prompt, got %q", req.Agent.Prompt)
	}
}

func TestSessionCreateSchemaDefaultAgent(t *testing.T) {
	req := SessionCreateRequest{Path: "/tmp/repo"}
	if issues := SessionCreateSchema.Validate(&req); len(issues) > 0 {
		t.Fatalf("expected no issues, got %v", issues)
	}
	if req.Agent == nil {
		t.Fatalf("expected agent defaulted")
	}
	if strings.TrimSpace(req.Agent.Model) == "" {
		t.Fatalf("expected default model to be set")
	}
}

func TestSessionCreateSchemaRequiresPath(t *testing.T) {
	req := SessionCreateRequest{}
	if issues := SessionCreateSchema.Validate(&req); len(issues) == 0 {
		t.Fatalf("expected validation issues")
	}
}

func TestSessionDeleteSchema(t *testing.T) {
	req := SessionDeleteRequest{}
	if issues := SessionDeleteSchema.Validate(&req); len(issues) == 0 {
		t.Fatalf("expected validation issues")
	}

	req = SessionDeleteRequest{Path: "  /tmp/worktree  "}
	if issues := SessionDeleteSchema.Validate(&req); len(issues) > 0 {
		t.Fatalf("expected no issues, got %v", issues)
	}
	if req.Path != "/tmp/worktree" {
		t.Fatalf("expected trimmed path, got %q", req.Path)
	}
}
