package sessionevents

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	coredb "github.com/Oudwins/droner/pkgs/droner/dronerd/db"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/internals/backends"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/eventlog"
)

func (s *System) handleQueuedEvent(ctx context.Context, evt eventlog.Envelope) error {
	state, err := s.loadSessionState(ctx, string(evt.StreamID))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		switch state.LifecycleState {
		case LifecycleStateReady, LifecycleStateEnvironmentProvisioningStarted, LifecycleStateEnvironmentProvisioningSuccess:
			return nil
		}
	}

	queued, err := decodeQueuedPayload(evt)
	if err != nil {
		return err
	}

	_, err = s.appendEvent(ctx, string(evt.StreamID), eventTypeSessionEnvironmentProvisioningStarted, provisioningStepPayload(queued.Branch, provisioningModeInitial), string(evt.ID), string(evt.StreamID))
	return err
}

func (s *System) handleHydrationRequested(ctx context.Context, evt eventlog.Envelope) error {
	state, err := s.loadSessionState(ctx, string(evt.StreamID))
	if err != nil {
		return err
	}

	var nextType eventlog.EventType
	var nextPayload any
	switch state.LifecycleState {
	case LifecycleStateQueued:
		nextType = eventTypeSessionEnvironmentProvisioningStarted
		nextPayload = provisioningStepPayload(state.Branch, provisioningModeInitial)
	case LifecycleStateEnvironmentProvisioningStarted:
		nextType = eventTypeSessionEnvironmentProvisioningStarted
		nextPayload = provisioningStepPayload(state.Branch, provisioningModeInitial)
	case LifecycleStateReady:
		nextType = eventTypeSessionEnvironmentProvisioningStarted
		nextPayload = provisioningStepPayload(state.Branch, provisioningModeRestart)
	case LifecycleStateCompletionRequested, LifecycleStateCompletionStarted:
		nextType = eventTypeSessionCompletionStarted
		nextPayload = requestStepPayload(state.Branch)
	case LifecycleStateDeletionRequested, LifecycleStateDeletionStarted:
		nextType = eventTypeSessionDeletionStarted
		nextPayload = requestStepPayload(state.Branch)
	default:
		return nil
	}

	_, err = s.appendEvent(ctx, string(evt.StreamID), nextType, nextPayload, string(evt.ID), string(evt.StreamID))
	return err
}

func (s *System) handleProvisioningStarted(ctx context.Context, evt eventlog.Envelope) error {
	state, err := s.loadSessionState(ctx, string(evt.StreamID))
	if err != nil {
		return err
	}
	payload, err := decodeProvisioningPayload(evt)
	if err != nil {
		return err
	}

	agentConfig, err := s.agentConfigFromJSON(conf.HarnessID(state.Harness), state.AgentConfig)
	if err != nil {
		return s.appendProvisioningFailure(ctx, evt, fmt.Errorf("failed to decode agent config: %w", err))
	}

	backend, err := s.backends.Get(conf.BackendID(state.BackendID))
	if err != nil {
		return s.appendProvisioningFailure(ctx, evt, fmt.Errorf("failed to resolve backend: %w", err))
	}

	if payload.Mode == provisioningModeRestart {
		result, hydrateErr := backend.HydrateSession(ctx, coredb.Session{
			ID:           state.StreamID,
			Harness:      state.Harness,
			Branch:       state.Branch,
			Status:       coredb.SessionStatusActiveIdle,
			BackendID:    state.BackendID,
			RepoPath:     state.RepoPath,
			RemoteUrl:    sql.NullString{String: state.RemoteURL, Valid: state.RemoteURL != ""},
			WorktreePath: state.WorktreePath,
			AgentConfig:  sql.NullString{String: state.AgentConfig, Valid: state.AgentConfig != ""},
		}, agentConfig)
		if hydrateErr != nil {
			return s.appendProvisioningFailure(ctx, evt, hydrateErr)
		}
		if result.Status != coredb.SessionStatusActiveIdle {
			message := result.Error
			if message == "" {
				message = fmt.Sprintf("hydration returned %s", result.Status)
			}
			return s.appendProvisioningFailure(ctx, evt, errors.New(message))
		}
	} else {
		reusableRefs, err := s.listReusableProjectionRefs(ctx, state.RepoPath, state.BackendID)
		if err != nil {
			return s.appendProvisioningFailure(ctx, evt, err)
		}
		cleanupCandidates := make([]backends.ReusableWorktreeCandidate, 0)
		nextIndex := 0
		if createErr := backend.CreateSession(ctx, state.RepoPath, state.WorktreePath, state.Branch, agentConfig, backends.CreateSessionOptions{
			NextReusableWorktree: func(context.Context) (*backends.ReusableWorktreeCandidate, error) {
				if nextIndex >= len(reusableRefs) {
					return nil, nil
				}
				ref := reusableRefs[nextIndex]
				nextIndex++
				return &backends.ReusableWorktreeCandidate{
					StreamID:     ref.StreamID,
					Branch:       ref.Branch,
					RepoPath:     ref.RepoPath,
					WorktreePath: ref.WorktreePath,
				}, nil
			},
			MarkReusableWorktreeDeletion: func(candidate backends.ReusableWorktreeCandidate) {
				cleanupCandidates = append(cleanupCandidates, candidate)
			},
		}); createErr != nil {
			return s.appendProvisioningFailure(ctx, evt, createErr)
		}
		for _, candidate := range cleanupCandidates {
			if candidate.StreamID == "" || candidate.Branch == "" {
				continue
			}
			if _, err := s.appendEvent(ctx, candidate.StreamID, eventTypeSessionDeletionRequested, requestStepPayload(candidate.Branch), string(evt.ID), string(evt.StreamID)); err != nil {
				return err
			}
		}
	}

	for _, eventType := range []eventlog.EventType{eventTypeSessionEnvironmentProvisioningSuccess, eventTypeSessionReady} {
		if _, err := s.appendEvent(ctx, string(evt.StreamID), eventType, requestStepPayload(state.Branch), string(evt.ID), string(evt.StreamID)); err != nil {
			return err
		}
	}
	return nil
}

func (s *System) appendProvisioningFailure(ctx context.Context, cause eventlog.Envelope, causeErr error) error {
	_, err := s.appendEvent(ctx, string(cause.StreamID), eventTypeSessionEnvironmentProvisioningFailed, newFailedPayload(causeErr), string(cause.ID), string(cause.StreamID))
	return err
}

func (s *System) handleCompletionRequested(ctx context.Context, evt eventlog.Envelope) error {
	state, err := s.loadSessionState(ctx, string(evt.StreamID))
	if err != nil {
		return err
	}
	if state.LifecycleState == LifecycleStateCompletionStarted || state.LifecycleState == LifecycleStateCompletionSuccess || state.LifecycleState == LifecycleStateDeletionSuccess {
		return nil
	}
	payload, err := decodeBranchPayload(evt)
	if err != nil {
		return err
	}
	_, err = s.appendEvent(ctx, string(evt.StreamID), eventTypeSessionCompletionStarted, requestStepPayload(payload.Branch), string(evt.ID), string(evt.StreamID))
	return err
}

func (s *System) handleCompletionStarted(ctx context.Context, evt eventlog.Envelope) error {
	state, err := s.loadSessionState(ctx, string(evt.StreamID))
	if err != nil {
		return err
	}
	if state.LifecycleState == LifecycleStateCompletionSuccess || state.LifecycleState == LifecycleStateDeletionSuccess {
		return nil
	}
	backend, err := s.backends.Get(conf.BackendID(state.BackendID))
	if err != nil {
		return s.appendCompletionFailure(ctx, evt, err)
	}
	if err := backend.CompleteSession(ctx, state.WorktreePath, state.Branch); err != nil {
		return s.appendCompletionFailure(ctx, evt, err)
	}
	_, err = s.appendEvent(ctx, string(evt.StreamID), eventTypeSessionCompletionSuccess, requestStepPayload(state.Branch), string(evt.ID), string(evt.StreamID))
	return err
}

func (s *System) handleDeletionRequested(ctx context.Context, evt eventlog.Envelope) error {
	state, err := s.loadSessionState(ctx, string(evt.StreamID))
	if err != nil {
		return err
	}
	if state.LifecycleState == LifecycleStateDeletionStarted || state.LifecycleState == LifecycleStateDeletionSuccess {
		return nil
	}
	payload, err := decodeBranchPayload(evt)
	if err != nil {
		return err
	}
	_, err = s.appendEvent(ctx, string(evt.StreamID), eventTypeSessionDeletionStarted, requestStepPayload(payload.Branch), string(evt.ID), string(evt.StreamID))
	return err
}

func (s *System) handleDeletionStarted(ctx context.Context, evt eventlog.Envelope) error {
	state, err := s.loadSessionState(ctx, string(evt.StreamID))
	if err != nil {
		return err
	}
	if state.LifecycleState == LifecycleStateDeletionSuccess {
		return nil
	}
	backend, err := s.backends.Get(conf.BackendID(state.BackendID))
	if err != nil {
		return s.appendDeletionFailure(ctx, evt, err)
	}
	if err := backend.DeleteSession(ctx, state.WorktreePath, state.Branch); err != nil {
		return s.appendDeletionFailure(ctx, evt, err)
	}
	_, err = s.appendEvent(ctx, string(evt.StreamID), eventTypeSessionDeletionSuccess, requestStepPayload(state.Branch), string(evt.ID), string(evt.StreamID))
	return err
}

func (s *System) appendCompletionFailure(ctx context.Context, cause eventlog.Envelope, causeErr error) error {
	_, err := s.appendEvent(ctx, string(cause.StreamID), eventTypeSessionCompletionFailed, newFailedPayload(causeErr), string(cause.ID), string(cause.StreamID))
	return err
}

func (s *System) appendDeletionFailure(ctx context.Context, cause eventlog.Envelope, causeErr error) error {
	_, err := s.appendEvent(ctx, string(cause.StreamID), eventTypeSessionDeletionFailed, newFailedPayload(causeErr), string(cause.ID), string(cause.StreamID))
	return err
}
