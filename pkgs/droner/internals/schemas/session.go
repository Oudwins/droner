package schemas

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Oudwins/droner/pkgs/droner/internals/backends"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"

	z "github.com/Oudwins/zog"
)

const (
	SimpleSessionDelimiter = ".."
)

func NewSSessionID(s string) SSessionID {
	return SSessionID(strings.ReplaceAll(s, ".", "/"))
}

// Simple Session ID
type SSessionID string

func sessionID() *z.StringSchema[SSessionID] {
	return z.StringLike[SSessionID]()
}

func (i SSessionID) String() string {
	return string(i)
}

// file system safe version of simpleID
func (i SSessionID) FSsafe() string {
	return strings.ReplaceAll(string(i), "/", ".")
}

// Folder name for the sesssion
func (i SSessionID) SessionWorktreeName(repoName string) string {
	return repoName + SimpleSessionDelimiter + string(i)
}

type SessionAgentConfig struct {
	Model  string `json:"model" zog:"model"`
	Prompt string `json:"prompt" zog:"prompt"`
}

type SessionCreateRequest struct {
	Path        string              `json:"path"`
	SessionID   SSessionID          `json:"sessionId,omitempty" zog:"sessionId"`
	BackendID   backends.BackendID  `json:"backendId,omitempty" zog:"backendId"`
	AgentConfig *SessionAgentConfig `json:"agentConfig,omitempty"`
}

var sessionIDRegex = regexp.MustCompile(`^[A-Za-z0-9/\-]+$`)
var multiupleSlashes = regexp.MustCompile(`//+`)

var SessionCreateSchema = z.Struct(z.Shape{
	"Path":      z.String().Required().Trim().Transform(cleanPathTransform),
	"SessionID": sessionID().Optional().Trim().Match(sessionIDRegex).Not().Match(multiupleSlashes),
	"BackendID": z.StringLike[backends.BackendID]().Default(conf.GetConfig().Sessions.DefaultBackend).OneOf(backends.AllBackendIDs),
	"AgentConfig": z.Ptr(z.Struct(z.Shape{
		"Model":  z.String().Default(conf.GetConfig().Sessions.Agent.DefaultModel).Trim(),
		"Prompt": z.String().Optional().Trim(),
	})),
})

type SessionCreateResponse struct {
	SessionID    SSessionID         `json:"sessionId"`
	SimpleID     string             `json:"simpleId"`
	BackendID    backends.BackendID `json:"backendId"`
	WorktreePath string             `json:"worktreePath"`
	TaskID       string             `json:"taskId"`
}

type SessionDeleteRequest struct {
	SessionID SSessionID `json:"sessionId" zog:"sessionId"`
}

type SessionDeleteResponse struct {
	SessionID SSessionID `json:"sessionId"`
	TaskId    string     `json:"taskId"`
}

type SessionListItem struct {
	SimpleID SSessionID `json:"simpleId"`
	State    string     `json:"state"`
}

type SessionListResponse struct {
	Sessions []SessionListItem `json:"sessions"`
}

var SessionDeleteSchema = z.Struct(z.Shape{
	"SessionID": sessionID().Optional().Trim(),
})

func cleanPathTransform(valPtr *string, c z.Ctx) error {
	*valPtr = filepath.Clean(*valPtr)
	return nil
}
