package schemas

import (
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/messages"

	z "github.com/Oudwins/zog"
)

const (
	SimpleSessionDelimiter = ".."
)

func NewSBranch(s string) SBranch {
	return SBranch(strings.ReplaceAll(s, ".", "/"))
}

type SBranch string

func branch() *z.StringSchema[SBranch] {
	return z.StringLike[SBranch]()
}

func (b SBranch) String() string {
	return string(b)
}

type SessionAgentConfig struct {
	Model     string                      `json:"model" zog:"model"`
	AgentName string                      `json:"agentName,omitempty" zog:"agentName"`
	Message   *messages.Message           `json:"message,omitempty"`
	Command   *messages.CommandInvocation `json:"command,omitempty"`
}

type SessionCreateRequest struct {
	Path        string              `json:"path"`
	Harness     conf.HarnessID      `json:"harness,omitempty" zog:"harness"`
	Branch      SBranch             `json:"branch,omitempty" zog:"branch"`
	BackendID   conf.BackendID      `json:"backendId,omitempty" zog:"backendId"`
	AgentConfig *SessionAgentConfig `json:"agentConfig,omitempty"`
}

func (r SessionCreateRequest) LogValue() slog.Value {
	attrs := []slog.Attr{
		slog.String("path", r.Path),
		slog.String("harness", r.Harness.String()),
		slog.String("branch", r.Branch.String()),
		slog.String("backendId", string(r.BackendID)),
	}

	if r.AgentConfig != nil {
		attrs = append(attrs, slog.Any("agentConfig", r.AgentConfig))
	}

	return slog.GroupValue(attrs...)
}

func (c SessionAgentConfig) LogValue() slog.Value {
	attrs := []slog.Attr{
		slog.String("model", c.Model),
	}

	if c.AgentName != "" {
		attrs = append(attrs, slog.String("agentName", c.AgentName))
	}

	if c.Message != nil {
		attrs = append(attrs, slog.Any("message", c.Message))
	}
	if c.Command != nil {
		attrs = append(attrs, slog.Any("command", c.Command))
	}

	return slog.GroupValue(attrs...)
}

func (c SessionAgentConfig) ToDescription() string {
	if c.Command != nil {
		return c.Command.InvocationText()
	}
	return messages.ToRawText(c.Message)
}

var branchRegex = regexp.MustCompile(`^[A-Za-z0-9/\-]+$`)
var multiupleSlashes = regexp.MustCompile(`//+`)

var SessionCreateSchema = z.Struct(z.Shape{
	"Path":      z.String().Required().Trim().Transform(cleanPathTransform),
	"Harness":   conf.HarnessIDSchema,
	"Branch":    branch().Optional().Trim().Match(branchRegex).Not().Match(multiupleSlashes),
	"BackendID": conf.BackendIDSchema,
	"AgentConfig": z.Ptr(z.Struct(z.Shape{
		"Model":     z.String().Default(conf.GetConfig().Sessions.Harness.DefaultModel()).Trim(),
		"AgentName": z.String().Optional().Trim(),
		"Message":   z.Ptr(messages.MessageSchema),
		"Command":   z.Ptr(messages.CommandInvocationSchema),
	})),
})

type SessionCreateResponse struct {
	ID           string         `json:"id"`
	Harness      conf.HarnessID `json:"harness"`
	Branch       SBranch        `json:"branch"`
	BackendID    conf.BackendID `json:"backendId"`
	WorktreePath string         `json:"worktreePath"`
	TaskID       string         `json:"taskId"`
}

type SessionDeleteRequest struct {
	Branch SBranch `json:"branch" zog:"branch"`
}

type SessionListItem struct {
	ID        string  `json:"id"`
	Repo      string  `json:"repo"`
	RemoteURL string  `json:"remoteUrl"`
	Branch    SBranch `json:"branch"`
	State     string  `json:"state"`
}

type SessionListResponse struct {
	Sessions []SessionListItem `json:"sessions"`
}

type SessionListDirection string

const (
	SessionListDirectionBefore SessionListDirection = "before"
	SessionListDirectionAfter  SessionListDirection = "after"
)

var SessionDeleteSchema = z.Struct(z.Shape{
	"Branch": branch().Required().Trim(),
})

type SessionCompleteRequest struct {
	Branch SBranch `json:"branch" zog:"branch"`
}

var SessionCompleteSchema = z.Struct(z.Shape{
	"Branch": branch().Required().Trim(),
})

// SessionListQuery represents query parameters accepted by GET /sessions.
type SessionListQuery struct {
	Status    []string             `zog:"status"`
	Limit     int                  `zog:"limit"`
	Cursor    string               `zog:"cursor"`
	Direction SessionListDirection `zog:"direction"`
}

type SessionNavigationQuery struct {
	ID     string  `zog:"id"`
	Branch SBranch `zog:"branch"`
}

var SessionListQuerySchema = z.Struct(z.Shape{
	"Status":    z.Slice(z.String().Min(1).Required()).Optional(),
	"Limit":     z.Int().Default(100).GTE(1),
	"Cursor":    z.String().Optional(),
	"Direction": z.StringLike[SessionListDirection]().OneOf([]SessionListDirection{SessionListDirectionBefore, SessionListDirectionAfter}).Default(SessionListDirectionAfter),
})

var SessionNavigationQuerySchema = z.Struct(z.Shape{
	"ID":     z.String().Optional().Trim(),
	"Branch": branch().Optional().Trim().Match(branchRegex).Not().Match(multiupleSlashes),
})

func cleanPathTransform(valPtr *string, c z.Ctx) error {
	*valPtr = filepath.Clean(*valPtr)
	return nil
}
