package sessionevents

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

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
		case string(eventTypeSessionReady), string(eventTypeSessionEnvironmentProvisioningFailed):
			return nil
		}
	}

	queued, err := decodeQueuedPayload(evt)
	if err != nil {
		return err
	}

	if _, err := s.appendEvent(ctx, string(evt.StreamID), eventTypeSessionEnvironmentProvisioningStarted, provisionStartedPayload(queued), string(evt.ID), string(evt.StreamID)); err != nil {
		return err
	}

	backend, err := s.backends.Get(queuedBackendID(queued))
	if err != nil {
		return s.appendFailure(ctx, evt, fmt.Errorf("failed to resolve backend: %w", err))
	}

	agentConfig, err := s.agentConfigFromJSON(queued.AgentConfigJSON)
	if err != nil {
		return s.appendFailure(ctx, evt, fmt.Errorf("failed to decode agent config: %w", err))
	}

	if err := backend.CreateSession(ctx, queued.RepoPath, queued.WorktreePath, queued.SimpleID, agentConfig); err != nil {
		return s.appendFailure(ctx, evt, err)
	}

	for _, eventType := range []eventlog.EventType{eventTypeSessionEnvironmentProvisioningSuccess, eventTypeSessionReady} {
		if _, err := s.appendEvent(ctx, string(evt.StreamID), eventType, readyStepPayload(queued), string(evt.ID), string(evt.StreamID)); err != nil {
			return err
		}
	}
	return nil
}

func (s *System) appendFailure(ctx context.Context, cause eventlog.Envelope, causeErr error) error {
	_, err := s.appendEvent(ctx, string(cause.StreamID), eventTypeSessionEnvironmentProvisioningFailed, newFailedPayload(causeErr), string(cause.ID), string(cause.StreamID))
	return err
}

func (s *System) handleCompletionRequested(ctx context.Context, evt eventlog.Envelope) error {
	projection, err := s.loadProjection(ctx, string(evt.StreamID))
	if err != nil {
		return err
	}
	if projection.LifecycleState == string(eventTypeSessionCompletionSuccess) || projection.LifecycleState == string(eventTypeSessionDeletionSuccess) {
		return nil
	}
	payload, err := decodeSessionIDPayload(evt)
	if err != nil {
		return err
	}
	if _, err := s.appendEvent(ctx, string(evt.StreamID), eventTypeSessionCompletionStarted, requestStepPayload(payload.SimpleID), string(evt.ID), string(evt.StreamID)); err != nil {
		return err
	}
	backend, err := s.backends.Get(conf.BackendID(projection.BackendID))
	if err != nil {
		return s.appendCompletionFailure(ctx, evt, err)
	}
	if err := backend.CompleteSession(ctx, projection.WorktreePath, projection.SimpleID); err != nil {
		return s.appendCompletionFailure(ctx, evt, err)
	}
	_, err = s.appendEvent(ctx, string(evt.StreamID), eventTypeSessionCompletionSuccess, requestStepPayload(projection.SimpleID), string(evt.ID), string(evt.StreamID))
	return err
}

func (s *System) handleDeletionRequested(ctx context.Context, evt eventlog.Envelope) error {
	projection, err := s.loadProjection(ctx, string(evt.StreamID))
	if err != nil {
		return err
	}
	if projection.LifecycleState == string(eventTypeSessionDeletionSuccess) {
		return nil
	}
	payload, err := decodeSessionIDPayload(evt)
	if err != nil {
		return err
	}
	if _, err := s.appendEvent(ctx, string(evt.StreamID), eventTypeSessionDeletionStarted, requestStepPayload(payload.SimpleID), string(evt.ID), string(evt.StreamID)); err != nil {
		return err
	}
	backend, err := s.backends.Get(conf.BackendID(projection.BackendID))
	if err != nil {
		return s.appendDeletionFailure(ctx, evt, err)
	}
	if err := backend.DeleteSession(ctx, projection.WorktreePath, projection.SimpleID); err != nil {
		return s.appendDeletionFailure(ctx, evt, err)
	}
	_, err = s.appendEvent(ctx, string(evt.StreamID), eventTypeSessionDeletionSuccess, requestStepPayload(projection.SimpleID), string(evt.ID), string(evt.StreamID))
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
