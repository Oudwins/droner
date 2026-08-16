package sessions

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/events/eventtypes"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/internals/naming"
)

type State struct {
	StreamID        string
	Harness         string
	RequestedBranch string
	Branch          string
	TmuxSessionName string
	BackendID       string
	RepoPath        string
	WorktreePath    string
	RemoteURL       string
	AgentConfig     string
	LifecycleState  LifecycleState
	PublicState     PublicState
	LastError       string
	PRNumber        int64
	PRState         string
	PRCIState       string
	PRUpdatedAt     time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func DeriveNames(repoPath string, worktreeDir string, branch string) (string, string, error) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "", "", errors.New("branch is required")
	}
	repoName := filepath.Base(repoPath)
	if strings.TrimSpace(repoName) == "" || repoName == "." || repoName == string(filepath.Separator) {
		return "", "", errors.New("repo path is required")
	}
	repoName = delimiterSafeRepoName(repoName)
	physicalName := physicalSessionName(branch)
	return fmt.Sprintf("%s#%s", repoName, physicalName), filepath.Join(worktreeDir, fmt.Sprintf("%s..%s", repoName, physicalName)), nil
}

type PublicState string

const (
	PublicStateQueued     PublicState = "queued"
	PublicStateActiveIdle PublicState = "active.idle"
	PublicStateActiveBusy PublicState = "active.busy"
	PublicStateCompleting PublicState = "completing"
	PublicStateCompleted  PublicState = "completed"
	PublicStateFailed     PublicState = "failed"
	PublicStateDeleting   PublicState = "deleting"
	PublicStateDeleted    PublicState = "deleted"
)

func (s PublicState) String() string {
	return string(s)
}

func (s PublicState) IsActive() bool {
	switch s {
	case PublicStateActiveIdle, PublicStateActiveBusy:
		return true
	default:
		return false
	}
}

func (s PublicState) IsTerminal() bool {
	switch s {
	case PublicStateCompleted, PublicStateFailed, PublicStateDeleted:
		return true
	default:
		return false
	}
}

type LifecycleState string

const (
	LifecycleStateQueued                         LifecycleState = LifecycleState(eventtypes.SessionQueued)
	LifecycleStateEnrichmentRequested            LifecycleState = LifecycleState(eventtypes.SessionEnrichmentRequested)
	LifecycleStateEnrichmentSucceeded            LifecycleState = LifecycleState(eventtypes.SessionEnrichmentSucceeded)
	LifecycleStateEnrichmentFailed               LifecycleState = LifecycleState(eventtypes.SessionEnrichmentFailed)
	LifecycleStateHydrationRequested             LifecycleState = LifecycleState(eventtypes.SessionHydrationRequested)
	LifecycleStateEnvironmentProvisioningStarted LifecycleState = LifecycleState(eventtypes.SessionEnvironmentProvisioningStarted)
	LifecycleStateEnvironmentProvisioningSuccess LifecycleState = LifecycleState(eventtypes.SessionEnvironmentProvisioningSuccess)
	LifecycleStateEnvironmentProvisioningFailed  LifecycleState = LifecycleState(eventtypes.SessionEnvironmentProvisioningFailed)
	LifecycleStateReady                          LifecycleState = LifecycleState(eventtypes.SessionReady)
	LifecycleStateCompletionRequested            LifecycleState = LifecycleState(eventtypes.SessionCompletionRequested)
	LifecycleStateCompletionStarted              LifecycleState = LifecycleState(eventtypes.SessionCompletionStarted)
	LifecycleStateCompletionSuccess              LifecycleState = LifecycleState(eventtypes.SessionCompletionSuccess)
	LifecycleStateCompletionFailed               LifecycleState = LifecycleState(eventtypes.SessionCompletionFailed)
	LifecycleStateDeletionRequested              LifecycleState = LifecycleState(eventtypes.SessionDeletionRequested)
	LifecycleStateDeletionStarted                LifecycleState = LifecycleState(eventtypes.SessionDeletionStarted)
	LifecycleStateDeletionSuccess                LifecycleState = LifecycleState(eventtypes.SessionDeletionSuccess)
	LifecycleStateDeletionFailed                 LifecycleState = LifecycleState(eventtypes.SessionDeletionFailed)
)

func (s LifecycleState) String() string {
	return string(s)
}

func (s LifecycleState) AllowsAgentRuntime() bool {
	return s == LifecycleStateReady
}

func (s LifecycleState) IsTerminal() bool {
	switch s {
	case LifecycleStateCompletionSuccess, LifecycleStateEnvironmentProvisioningFailed, LifecycleStateCompletionFailed, LifecycleStateDeletionSuccess, LifecycleStateDeletionFailed:
		return true
	default:
		return false
	}
}

func delimiterSafeRepoName(repoName string) string {
	repoName = strings.TrimSpace(repoName)
	repoName = strings.ReplaceAll(repoName, "#", "-")
	for strings.Contains(repoName, "..") {
		repoName = strings.ReplaceAll(repoName, "..", ".")
	}
	repoName = strings.Trim(repoName, ".")
	if repoName == "" {
		return "repo"
	}
	return repoName
}

func physicalSessionName(branch string) string {
	name := naming.SanitizeSessionNamePrefix(branch)
	if name == "" {
		name = "session"
	}
	if name == branch {
		return name
	}
	hash := sha1.Sum([]byte(branch))
	return fmt.Sprintf("%s-%s", name, hex.EncodeToString(hash[:])[:6])
}
