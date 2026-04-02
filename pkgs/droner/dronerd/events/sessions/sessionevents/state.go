package sessionevents

import (
	"context"
	"database/sql"
	"time"

	coredb "github.com/Oudwins/droner/pkgs/droner/dronerd/db"
	"github.com/Oudwins/droner/pkgs/droner/internals/eventlog"
)

type sessionState struct {
	StreamID       string
	Harness        string
	Branch         string
	BackendID      string
	RepoPath       string
	WorktreePath   string
	RemoteURL      string
	AgentConfig    string
	LifecycleState LifecycleState
	PublicState    PublicState
	LastError      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (s *System) loadSessionState(ctx context.Context, streamID string) (sessionState, error) {
	state, count, err := s.loadSessionStateBeforeVersion(ctx, streamID, 0)
	if err != nil {
		return sessionState{}, err
	}
	if count == 0 {
		return sessionState{}, sql.ErrNoRows
	}
	return state, nil
}

func (s *System) loadSessionStateBeforeVersion(ctx context.Context, streamID string, beforeVersion int64) (sessionState, int, error) {
	events, err := s.log.LoadStream(ctx, eventlog.StreamID(streamID), eventlog.LoadStreamOptions{})
	if err != nil {
		return sessionState{}, 0, err
	}

	var state sessionState
	count := 0
	for _, evt := range events {
		if beforeVersion > 0 && evt.StreamVersion >= beforeVersion {
			break
		}
		if _, err := state.Apply(evt); err != nil {
			return sessionState{}, 0, err
		}
		count++
	}

	return state, count, nil
}

func (s *sessionState) Hydrate(events []eventlog.Envelope) error {
	for _, evt := range events {
		if _, err := s.Apply(evt); err != nil {
			return err
		}
	}
	return nil
}

func (s *sessionState) Apply(evt eventlog.Envelope) (bool, error) {
	switch evt.Type {
	case eventTypeSessionQueued:
		payload, err := decodeQueuedPayload(evt)
		if err != nil {
			return false, err
		}
		s.StreamID = string(evt.StreamID)
		s.Harness = payload.Harness
		s.Branch = payload.Branch
		s.BackendID = payload.BackendID
		s.RepoPath = payload.RepoPath
		s.WorktreePath = payload.WorktreePath
		s.RemoteURL = payload.RemoteURL
		s.AgentConfig = payload.AgentConfigJSON
		s.transition(LifecycleStateQueued, PublicStateQueued, "", evt.OccurredAt)
		if s.CreatedAt.IsZero() {
			s.CreatedAt = evt.OccurredAt.UTC()
		}
		return true, nil
	case eventTypeSessionHydrationRequested:
		return false, nil
	case eventTypeSessionEnvironmentProvisioningStarted:
		s.transition(LifecycleStateEnvironmentProvisioningStarted, PublicStateQueued, "", evt.OccurredAt)
		return true, nil
	case eventTypeSessionEnvironmentProvisioningSuccess:
		s.transition(LifecycleStateEnvironmentProvisioningSuccess, PublicStateQueued, "", evt.OccurredAt)
		return true, nil
	case eventTypeSessionReady:
		s.transition(LifecycleStateReady, PublicStateActiveIdle, "", evt.OccurredAt)
		return true, nil
	case eventTypeSessionAgentBusy:
		return s.applyAgentState(PublicStateActiveBusy, evt.OccurredAt), nil
	case eventTypeSessionAgentIdle:
		return s.applyAgentState(PublicStateActiveIdle, evt.OccurredAt), nil
	case eventTypeSessionEnvironmentProvisioningFailed:
		payload, err := decodeFailedPayload(evt)
		if err != nil {
			return false, err
		}
		s.transition(LifecycleStateEnvironmentProvisioningFailed, PublicStateFailed, payload.Error, evt.OccurredAt)
		return true, nil
	case eventTypeSessionCompletionRequested:
		s.transition(LifecycleStateCompletionRequested, s.publicStateForCompletionRequest(), "", evt.OccurredAt)
		return true, nil
	case eventTypeSessionCompletionStarted:
		s.transition(LifecycleStateCompletionStarted, PublicStateCompleting, "", evt.OccurredAt)
		return true, nil
	case eventTypeSessionCompletionSuccess:
		s.transition(LifecycleStateCompletionSuccess, PublicStateCompleted, "", evt.OccurredAt)
		return true, nil
	case eventTypeSessionCompletionFailed:
		payload, err := decodeFailedPayload(evt)
		if err != nil {
			return false, err
		}
		s.transition(LifecycleStateCompletionFailed, PublicStateFailed, payload.Error, evt.OccurredAt)
		return true, nil
	case eventTypeSessionDeletionRequested:
		s.transition(LifecycleStateDeletionRequested, PublicStateDeleting, "", evt.OccurredAt)
		return true, nil
	case eventTypeSessionDeletionStarted:
		s.transition(LifecycleStateDeletionStarted, PublicStateDeleting, "", evt.OccurredAt)
		return true, nil
	case eventTypeSessionDeletionSuccess:
		s.transition(LifecycleStateDeletionSuccess, PublicStateDeleted, "", evt.OccurredAt)
		return true, nil
	case eventTypeSessionDeletionFailed:
		payload, err := decodeFailedPayload(evt)
		if err != nil {
			return false, err
		}
		s.transition(LifecycleStateDeletionFailed, PublicStateFailed, payload.Error, evt.OccurredAt)
		return true, nil
	case eventTypeRemotePRClosed, eventTypeRemotePRMerged, eventTypeRemoteBranchDeleted:
		return false, nil
	default:
		return false, nil
	}
}

func (s *sessionState) publicStateForCompletionRequest() PublicState {
	if s.PublicState.IsActive() {
		return s.PublicState
	}
	return PublicStateActiveIdle
}

func (s *sessionState) applyAgentState(publicState PublicState, occurredAt time.Time) bool {
	if !s.LifecycleState.AllowsAgentRuntime() || s.PublicState.IsTerminal() || s.PublicState == publicState {
		return false
	}
	s.PublicState = publicState
	s.LastError = ""
	s.UpdatedAt = occurredAt.UTC()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = s.UpdatedAt
	}
	return true
}

func (s *sessionState) transition(lifecycleState LifecycleState, publicState PublicState, lastError string, occurredAt time.Time) {
	s.LifecycleState = lifecycleState
	s.PublicState = publicState
	s.LastError = lastError
	s.UpdatedAt = occurredAt.UTC()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = s.UpdatedAt
	}
}

func stateFromProjection(row coredb.SessionProjection) sessionState {
	return sessionState{
		StreamID:       row.StreamID,
		Harness:        row.Harness,
		Branch:         row.Branch,
		BackendID:      row.BackendID,
		RepoPath:       row.RepoPath,
		WorktreePath:   row.WorktreePath,
		RemoteURL:      row.RemoteUrl,
		AgentConfig:    row.AgentConfig,
		LifecycleState: LifecycleState(row.LifecycleState),
		PublicState:    PublicState(row.PublicState),
		LastError:      row.LastError,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
}

func (s sessionState) projectionMutation() projectionMutation {
	return projectionMutation{
		StreamID:       s.StreamID,
		Harness:        s.Harness,
		Branch:         s.Branch,
		BackendID:      s.BackendID,
		RepoPath:       s.RepoPath,
		WorktreePath:   s.WorktreePath,
		RemoteURL:      s.RemoteURL,
		AgentConfig:    s.AgentConfig,
		LifecycleState: s.LifecycleState.String(),
		PublicState:    s.PublicState.String(),
		LastError:      s.LastError,
		CreatedAt:      s.CreatedAt,
		UpdatedAt:      s.UpdatedAt,
	}
}
