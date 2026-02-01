package schemas

import (
	"path/filepath"

	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/workspace"

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

var SessionCreateSchema = z.Struct(z.Shape{
	"Path": z.String().Required().Trim().Transform(func(valPtr *string, ctx z.Ctx) error {
		*valPtr = filepath.Clean(*valPtr)
		return nil
	},
	).TestFunc(func(valPtr *string, ctx z.Ctx) bool {
		w, ok := ctx.Get("workspace").(workspace.Host)
		if !ok {
			ctx.AddIssue(ctx.Issue().SetMessage("Something wen't wrong trying to get workspace from context. Internal error"))
			return true
		}
		file, err := w.Stat(*valPtr)
		if err != nil {
			ctx.AddIssue(ctx.Issue().SetMessage("Failed to stat path"))
			return true
		}

		if !file.IsDir() {
			ctx.AddIssue(ctx.Issue().SetMessage("Path is not a directory"))
			return true
		}
		err = w.GitIsInsideWorkTree(*valPtr)
		if err != nil {
			ctx.AddIssue(ctx.Issue().SetMessage("Path is not to a git repo").SetError(err))
		}
		return true
	}, z.Message("Path is not a git repo")),
	"SessionID": z.String().Optional().Trim(),
	"Agent": z.Ptr(z.Struct(z.Shape{
		"Model":  z.String().Default(conf.GetConfig().Agent.DefaultModel).Trim(),
		"Prompt": z.String().Optional().Trim(),
	})),
})

type SessionCreateResponse struct {
	WorktreePath string `json:"worktreePath"`
	SessionID    string `json:"sessionId"`
	TaskID       string `json:"taskId"`
}

type SessionDeleteRequest struct {
	Path      string `json:"path" zog:"path"`
	SessionID string `json:"session_id" zog:"session_id"`
}

type SessionDeleteResponse struct {
	WorktreePath string `json:"worktree_path"`
	SessionID    string `json:"session_id"`
}

var SessionDeleteSchema = z.Struct(z.Shape{
	"Path":      z.String().Optional().Trim(),
	"SessionID": z.String().Optional().Trim(),
}).TestFunc(func(valPtr any, ctx z.Ctx) bool {
	v := valPtr.(*SessionDeleteRequest)
	return v.Path != "" || v.SessionID != ""
}, z.Message("At least one of path or sessionId are required"))
