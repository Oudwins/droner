package schemas

import (
	"path/filepath"
	"regexp"

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
	SessionID string              `json:"sessionId" zog:"sessionId"`
	Agent     *SessionAgentConfig `json:"agent" zog:"agent"`
}

var sessionIDRegex = regexp.MustCompile(`^[A-Za-z0-9/\-]+$`)
var multiupleSlashes = regexp.MustCompile(`//+`)

var SessionCreateSchema = z.Struct(z.Shape{
	"Path":      z.String().Required().Trim().Transform(cleanPathTransform).TestFunc(isGitRepoTest, z.Message("Path is not a git repo")),
	"SessionID": z.String().Optional().Trim().Match(sessionIDRegex).Not().Match(multiupleSlashes),
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
	SessionID string `json:"sessionId" zog:"sessionId"`
}

type SessionDeleteResponse struct {
	SessionID string `json:"sessionId"`
	TaskId    string `json:"taskId"`
}

var SessionDeleteSchema = z.Struct(z.Shape{
	"SessionID": z.String().Optional().Trim(),
})

func cleanPathTransform(valPtr *string, c z.Ctx) error {
	*valPtr = filepath.Clean(*valPtr)
	return nil
}

func isGitRepoTest(valPtr *string, ctx z.Ctx) bool {
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

}
