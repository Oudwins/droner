package schemas

type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusSucceeded TaskStatus = "succeeded"
)

type TaskResult struct {
	Branch       string `json:"branch,omitempty"`
	Requested    string `json:"requested,omitempty"`
	WorktreePath string `json:"worktreePath,omitempty"`
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
