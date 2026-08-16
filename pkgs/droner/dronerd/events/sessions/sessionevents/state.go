package sessionevents

import (
	"context"
	"database/sql"
	"time"

	coredb "github.com/Oudwins/droner/pkgs/droner/dronerd/db"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/events/eventtypes"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/events/sessions"
	"github.com/Oudwins/droner/pkgs/droner/internals/eventlog"
)

func (s *System) loadSessionState(ctx context.Context, streamID string) (sessions.State, error) {
	state, count, err := s.loadSessionStateBeforeVersion(ctx, streamID, 0)
	if err != nil {
		return sessions.State{}, err
	}
	if count == 0 {
		return sessions.State{}, sql.ErrNoRows
	}
	return state, nil
}

func (s *System) loadSessionStateBeforeVersion(ctx context.Context, streamID string, beforeVersion int64) (sessions.State, int, error) {
	events, err := s.log.LoadStream(ctx, eventlog.StreamID(streamID), eventlog.LoadStreamOptions{})
	if err != nil {
		return sessions.State{}, 0, err
	}

	var state sessions.State
	count := 0
	for _, evt := range events {
		if beforeVersion > 0 && evt.StreamVersion >= beforeVersion {
			break
		}
		if _, err := applySessionEvent(&state, evt); err != nil {
			return sessions.State{}, 0, err
		}
		count++
	}

	return state, count, nil
}

func applySessionEvent(s *sessions.State, evt eventlog.Envelope) (bool, error) {
	switch evt.Type {
	case eventtypes.SessionQueued:
		payload, err := decodeQueuedPayload(evt)
		if err != nil {
			return false, err
		}
		*s = sessions.State{
			StreamID:        string(evt.StreamID),
			Harness:         payload.Harness,
			RequestedBranch: payload.RequestedBranch,
			BackendID:       payload.BackendID,
			RepoPath:        payload.RepoPath,
			RemoteURL:       payload.RemoteURL,
			AgentConfig:     payload.AgentConfigJSON,
		}
		transition(s, sessions.LifecycleStateQueued, sessions.PublicStateQueued, "", evt.OccurredAt)
		if s.CreatedAt.IsZero() {
			s.CreatedAt = evt.OccurredAt.UTC()
		}
		return true, nil
	case eventtypes.SessionEnrichmentRequested:
		transition(s, sessions.LifecycleStateEnrichmentRequested, sessions.PublicStateQueued, "", evt.OccurredAt)
		return true, nil
	case eventtypes.SessionEnrichmentSucceeded:
		payload, err := decodeEnrichmentSucceededPayload(evt)
		if err != nil {
			return false, err
		}
		s.Branch = payload.Branch
		s.TmuxSessionName = payload.TmuxSessionName
		s.WorktreePath = payload.WorktreePath
		transition(s, sessions.LifecycleStateEnrichmentSucceeded, sessions.PublicStateQueued, "", evt.OccurredAt)
		return true, nil
	case eventtypes.SessionEnrichmentFailed:
		payload, err := decodeFailedPayload(evt)
		if err != nil {
			return false, err
		}
		transition(s, sessions.LifecycleStateEnrichmentFailed, sessions.PublicStateFailed, payload.Error, evt.OccurredAt)
		return true, nil
	case eventtypes.SessionHydrationRequested:
		return false, nil
	case eventtypes.SessionEnvironmentProvisioningStarted:
		transition(s, sessions.LifecycleStateEnvironmentProvisioningStarted, sessions.PublicStateQueued, "", evt.OccurredAt)
		return true, nil
	case eventtypes.SessionEnvironmentProvisioningSuccess:
		transition(s, sessions.LifecycleStateEnvironmentProvisioningSuccess, sessions.PublicStateQueued, "", evt.OccurredAt)
		return true, nil
	case eventtypes.SessionReady:
		transition(s, sessions.LifecycleStateReady, sessions.PublicStateActiveIdle, "", evt.OccurredAt)
		return true, nil
	case eventtypes.SessionAgentBusy:
		return applyAgentState(s, sessions.PublicStateActiveBusy, evt.OccurredAt), nil
	case eventtypes.SessionAgentIdle:
		return applyAgentState(s, sessions.PublicStateActiveIdle, evt.OccurredAt), nil
	case eventtypes.SessionEnvironmentProvisioningFailed:
		payload, err := decodeFailedPayload(evt)
		if err != nil {
			return false, err
		}
		transition(s, sessions.LifecycleStateEnvironmentProvisioningFailed, sessions.PublicStateFailed, payload.Error, evt.OccurredAt)
		return true, nil
	case eventtypes.SessionCompletionRequested:
		transition(s, sessions.LifecycleStateCompletionRequested, publicStateForCompletionRequest(s), "", evt.OccurredAt)
		return true, nil
	case eventtypes.SessionCompletionStarted:
		transition(s, sessions.LifecycleStateCompletionStarted, sessions.PublicStateCompleting, "", evt.OccurredAt)
		return true, nil
	case eventtypes.SessionCompletionSuccess:
		transition(s, sessions.LifecycleStateCompletionSuccess, sessions.PublicStateCompleted, "", evt.OccurredAt)
		return true, nil
	case eventtypes.SessionCompletionFailed:
		payload, err := decodeFailedPayload(evt)
		if err != nil {
			return false, err
		}
		transition(s, sessions.LifecycleStateCompletionFailed, sessions.PublicStateFailed, payload.Error, evt.OccurredAt)
		return true, nil
	case eventtypes.SessionDeletionRequested:
		transition(s, sessions.LifecycleStateDeletionRequested, sessions.PublicStateDeleting, "", evt.OccurredAt)
		return true, nil
	case eventtypes.SessionDeletionStarted:
		transition(s, sessions.LifecycleStateDeletionStarted, sessions.PublicStateDeleting, "", evt.OccurredAt)
		return true, nil
	case eventtypes.SessionDeletionSuccess:
		transition(s, sessions.LifecycleStateDeletionSuccess, sessions.PublicStateDeleted, "", evt.OccurredAt)
		return true, nil
	case eventtypes.SessionDeletionFailed:
		payload, err := decodeFailedPayload(evt)
		if err != nil {
			return false, err
		}
		transition(s, sessions.LifecycleStateDeletionFailed, sessions.PublicStateFailed, payload.Error, evt.OccurredAt)
		return true, nil
	case eventtypes.SessionPRLinked:
		payload, err := decodeSessionPRLinkedPayload(evt)
		if err != nil {
			return false, err
		}
		s.PRNumber = int64(payload.PRNumber)
		s.PRState = payload.State
		s.PRCIState = payload.CIState
		s.PRUpdatedAt = payload.LinkedAt.UTC()
		s.UpdatedAt = evt.OccurredAt.UTC()
		return true, nil
	case eventtypes.SessionPRStateChanged:
		payload, err := decodeSessionPRStateChangedPayload(evt)
		if err != nil {
			return false, err
		}
		s.PRNumber = int64(payload.PRNumber)
		s.PRState = payload.State
		s.PRUpdatedAt = payload.ChangedAt.UTC()
		s.UpdatedAt = evt.OccurredAt.UTC()
		return true, nil
	case eventtypes.SessionPRCIStateChanged:
		payload, err := decodeSessionPRCIStateChangedPayload(evt)
		if err != nil {
			return false, err
		}
		s.PRNumber = int64(payload.PRNumber)
		s.PRCIState = payload.CIState
		s.PRUpdatedAt = payload.ChangedAt.UTC()
		s.UpdatedAt = evt.OccurredAt.UTC()
		return true, nil
	case eventtypes.SessionPRClosed:
		payload, err := decodeSessionPRStateChangedPayload(evt)
		if err != nil {
			return false, err
		}
		s.PRNumber = int64(payload.PRNumber)
		s.PRState = "closed"
		s.PRUpdatedAt = payload.ChangedAt.UTC()
		s.UpdatedAt = evt.OccurredAt.UTC()
		return true, nil
	case eventtypes.SessionPRMerged:
		payload, err := decodeSessionPRStateChangedPayload(evt)
		if err != nil {
			return false, err
		}
		s.PRNumber = int64(payload.PRNumber)
		s.PRState = "merged"
		s.PRUpdatedAt = payload.ChangedAt.UTC()
		s.UpdatedAt = evt.OccurredAt.UTC()
		return true, nil
	default:
		return false, nil
	}
}

func publicStateForCompletionRequest(s *sessions.State) sessions.PublicState {
	if s.PublicState.IsActive() {
		return s.PublicState
	}
	return sessions.PublicStateActiveIdle
}

func applyAgentState(s *sessions.State, publicState sessions.PublicState, occurredAt time.Time) bool {
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

func transition(s *sessions.State, lifecycleState sessions.LifecycleState, publicState sessions.PublicState, lastError string, occurredAt time.Time) {
	s.LifecycleState = lifecycleState
	s.PublicState = publicState
	s.LastError = lastError
	s.UpdatedAt = occurredAt.UTC()
	if s.CreatedAt.IsZero() {
		s.CreatedAt = s.UpdatedAt
	}
}

func stateFromProjection(row coredb.SessionProjection) sessions.State {
	return sessions.State{
		StreamID:        row.StreamID,
		Harness:         row.Harness,
		Branch:          nullStringValue(row.Branch),
		TmuxSessionName: nullStringValue(row.TmuxSessionName),
		BackendID:       row.BackendID,
		RepoPath:        row.RepoPath,
		WorktreePath:    nullStringValue(row.WorktreePath),
		RemoteURL:       row.RemoteUrl,
		AgentConfig:     row.AgentConfig,
		LifecycleState:  sessions.LifecycleState(row.LifecycleState),
		PublicState:     sessions.PublicState(row.PublicState),
		LastError:       row.LastError,
		PRNumber:        nullInt64Value(row.PrNumber),
		PRState:         nullStringValue(row.PrState),
		PRCIState:       nullStringValue(row.PrCiState),
		PRUpdatedAt:     nullTimeValue(row.PrUpdatedAt),
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}
}
