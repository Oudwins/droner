package schemas

type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusSucceeded TaskStatus = "succeeded"
	TaskStatusFailed    TaskStatus = "failed"
)

type TaskResult struct {
	SessionID    string `json:"session_id,omitempty"`
	WorktreePath string `json:"worktree_path,omitempty"`
}

type TaskResponse struct {
	TaskID     string      `json:"task_id"`
	Type       string      `json:"type"`
	Status     TaskStatus  `json:"status"`
	CreatedAt  string      `json:"created_at"`
	StartedAt  string      `json:"started_at,omitempty"`
	FinishedAt string      `json:"finished_at,omitempty"`
	Error      string      `json:"error,omitempty"`
	Result     *TaskResult `json:"result,omitempty"`
}
