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
	projection, err := s.loadProjection(ctx, string(evt.StreamID))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		switch projection.LifecycleState {
		case string(eventTypeSessionReady), string(eventTypeSessionEnvironmentProvisioningStarted), string(eventTypeSessionEnvironmentProvisioningSuccess):
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
	projection, err := s.loadProjection(ctx, string(evt.StreamID))
	if err != nil {
		return err
	}

	var nextType eventlog.EventType
	var nextPayload any
	switch projection.LifecycleState {
	case string(eventTypeSessionQueued):
		nextType = eventTypeSessionEnvironmentProvisioningStarted
		nextPayload = provisioningStepPayload(projection.Branch, provisioningModeInitial)
	case string(eventTypeSessionEnvironmentProvisioningStarted):
		nextType = eventTypeSessionEnvironmentProvisioningStarted
		nextPayload = provisioningStepPayload(projection.Branch, provisioningModeInitial)
	case string(eventTypeSessionReady):
		nextType = eventTypeSessionEnvironmentProvisioningStarted
		nextPayload = provisioningStepPayload(projection.Branch, provisioningModeRestart)
	case string(eventTypeSessionCompletionRequested), string(eventTypeSessionCompletionStarted):
		nextType = eventTypeSessionCompletionStarted
		nextPayload = requestStepPayload(projection.Branch)
	case string(eventTypeSessionDeletionRequested), string(eventTypeSessionDeletionStarted):
		nextType = eventTypeSessionDeletionStarted
		nextPayload = requestStepPayload(projection.Branch)
	default:
		return nil
	}

	_, err = s.appendEvent(ctx, string(evt.StreamID), nextType, nextPayload, string(evt.ID), string(evt.StreamID))
	return err
}

func (s *System) handleProvisioningStarted(ctx context.Context, evt eventlog.Envelope) error {
	projection, err := s.loadProjection(ctx, string(evt.StreamID))
	if err != nil {
		return err
	}
	payload, err := decodeProvisioningPayload(evt)
	if err != nil {
		return err
	}

	agentConfig, err := s.agentConfigFromJSON(conf.HarnessID(projection.Harness), projection.AgentConfig)
	if err != nil {
		return s.appendProvisioningFailure(ctx, evt, fmt.Errorf("failed to decode agent config: %w", err))
	}

	backend, err := s.backends.Get(conf.BackendID(projection.BackendID))
	if err != nil {
		return s.appendProvisioningFailure(ctx, evt, fmt.Errorf("failed to resolve backend: %w", err))
	}

	if payload.Mode == provisioningModeRestart {
		result, hydrateErr := backend.HydrateSession(ctx, coredb.Session{
			ID:           projection.StreamID,
			Harness:      projection.Harness,
			Branch:       projection.Branch,
			Status:       coredb.SessionStatusRunning,
			BackendID:    projection.BackendID,
			RepoPath:     projection.RepoPath,
			RemoteUrl:    sql.NullString{String: projection.RemoteURL, Valid: projection.RemoteURL != ""},
			WorktreePath: projection.WorktreePath,
			AgentConfig:  sql.NullString{String: projection.AgentConfig, Valid: projection.AgentConfig != ""},
		}, agentConfig)
		if hydrateErr != nil {
			return s.appendProvisioningFailure(ctx, evt, hydrateErr)
		}
		if result.Status != coredb.SessionStatusRunning {
			message := result.Error
			if message == "" {
				message = fmt.Sprintf("hydration returned %s", result.Status)
			}
			return s.appendProvisioningFailure(ctx, evt, errors.New(message))
		}
	} else {
		reusableRefs, err := s.listReusableProjectionRefs(ctx, projection.RepoPath, projection.BackendID)
		if err != nil {
			return s.appendProvisioningFailure(ctx, evt, err)
		}
		cleanupCandidates := make([]backends.ReusableWorktreeCandidate, 0)
		nextIndex := 0
		if createErr := backend.CreateSession(ctx, projection.RepoPath, projection.WorktreePath, projection.Branch, agentConfig, backends.CreateSessionOptions{
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
		if _, err := s.appendEvent(ctx, string(evt.StreamID), eventType, requestStepPayload(projection.Branch), string(evt.ID), string(evt.StreamID)); err != nil {
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
	projection, err := s.loadProjection(ctx, string(evt.StreamID))
	if err != nil {
		return err
	}
	if projection.LifecycleState == string(eventTypeSessionCompletionStarted) || projection.LifecycleState == string(eventTypeSessionCompletionSuccess) || projection.LifecycleState == string(eventTypeSessionDeletionSuccess) {
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
	projection, err := s.loadProjection(ctx, string(evt.StreamID))
	if err != nil {
		return err
	}
	if projection.LifecycleState == string(eventTypeSessionCompletionSuccess) || projection.LifecycleState == string(eventTypeSessionDeletionSuccess) {
		return nil
	}
	backend, err := s.backends.Get(conf.BackendID(projection.BackendID))
	if err != nil {
		return s.appendCompletionFailure(ctx, evt, err)
	}
	if err := backend.CompleteSession(ctx, projection.WorktreePath, projection.Branch); err != nil {
		return s.appendCompletionFailure(ctx, evt, err)
	}
	_, err = s.appendEvent(ctx, string(evt.StreamID), eventTypeSessionCompletionSuccess, requestStepPayload(projection.Branch), string(evt.ID), string(evt.StreamID))
	return err
}

func (s *System) handleDeletionRequested(ctx context.Context, evt eventlog.Envelope) error {
	projection, err := s.loadProjection(ctx, string(evt.StreamID))
	if err != nil {
		return err
	}
	if projection.LifecycleState == string(eventTypeSessionDeletionStarted) || projection.LifecycleState == string(eventTypeSessionDeletionSuccess) {
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
	projection, err := s.loadProjection(ctx, string(evt.StreamID))
	if err != nil {
		return err
	}
	if projection.LifecycleState == string(eventTypeSessionDeletionSuccess) {
		return nil
	}
	backend, err := s.backends.Get(conf.BackendID(projection.BackendID))
	if err != nil {
		return s.appendDeletionFailure(ctx, evt, err)
	}
	if err := backend.DeleteSession(ctx, projection.WorktreePath, projection.Branch); err != nil {
		return s.appendDeletionFailure(ctx, evt, err)
	}
	_, err = s.appendEvent(ctx, string(evt.StreamID), eventTypeSessionDeletionSuccess, requestStepPayload(projection.Branch), string(evt.ID), string(evt.StreamID))
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
