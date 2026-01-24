package schemas

import (
	"droner/internals/conf"

	z "github.com/Oudwins/zog"
)

type SessionAgentConfig struct {
	Model  string `json:"model" zog:"model"`
	Prompt string `json:"prompt" zog:"prompt"`
}

type SessionCreateRequest struct {
	Path      string              `json:"path" zog:"path"`
	SessionID string              `json:"session_id" zog:"session_id"`
	Agent     *SessionAgentConfig `json:"agent,omitempty" zog:"agent"`
}

type SessionCreateResponse struct {
	WorktreePath string `json:"worktree_path"`
	SessionID    string `json:"session_id"`
}

type SessionDeleteRequest struct {
	Path      string `json:"path" zog:"path"`
	SessionID string `json:"session_id" zog:"session_id"`
}

type SessionDeleteResponse struct {
	WorktreePath string `json:"worktree_path"`
	SessionID    string `json:"session_id"`
}

var SessionCreateSchema = z.Struct(z.Shape{
	"Path":      z.String().Required().Trim(),
	"SessionID": z.String().Optional().Trim(),
	"Agent": z.Ptr(z.Struct(z.Shape{
		"Model":  z.String().Default(conf.GetConfig().DEFAULT_MODEL).Trim(),
		"Prompt": z.String().Optional().Trim(),
	})),
}).Transform(func(valPtr any, _ z.Ctx) error {
	request := valPtr.(*SessionCreateRequest)
	if request.Agent == nil {
		request.Agent = &SessionAgentConfig{Model: conf.GetConfig().DEFAULT_MODEL}
		return nil
	}
	if request.Agent.Model == "" {
		request.Agent.Model = conf.GetConfig().DEFAULT_MODEL
	}
	return nil
})

var SessionDeleteSchema = z.Struct(z.Shape{
	"Path":      z.String().Optional().Trim(),
	"SessionID": z.String().Optional().Trim(),
}).TestFunc(func(valPtr any, ctx z.Ctx) bool {
	v := valPtr.(*SessionDeleteRequest)
	return v.Path != "" || v.SessionID != ""
}, z.Message("At least one of path or sessionId are required"))
