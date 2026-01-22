package schemas

import z "github.com/Oudwins/zog"

type SessionCreateRequest struct {
	Path      string `json:"path" zog:"path"`
	SessionID string `json:"session_id" zog:"session_id"`
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
	"Path":      z.String().Required(),
	"SessionID": z.String().Optional(),
})

var SessionDeleteSchema = z.Struct(z.Shape{
	"Path":      z.String().Optional(),
	"SessionID": z.String().Optional(),
}).TestFunc(func(valPtr any, ctx z.Ctx) bool {
	v := valPtr.(*SessionDeleteRequest)
	return v.Path != "" || v.SessionID != ""
}, z.Message("At least one of path or sessionId are required"))
