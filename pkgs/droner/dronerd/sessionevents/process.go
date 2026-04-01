package sessionevents

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Oudwins/droner/pkgs/droner/internals/eventlog"
)

func (s *System) handleQueuedEvent(ctx context.Context, evt eventlog.Envelope) error {
	projection, err := s.loadProjection(ctx, string(evt.StreamID))
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil {
		switch projection.LifecycleState {
		case string(eventTypeSessionReady), string(eventTypeSessionFailed):
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

	for _, eventType := range []eventlog.EventType{eventTypeSessionEnvironmentProvisioned, eventTypeSessionRuntimeStarted, eventTypeSessionReady} {
		if _, err := s.appendEvent(ctx, string(evt.StreamID), eventType, readyStepPayload(queued), string(evt.ID), string(evt.StreamID)); err != nil {
			return err
		}
	}
	return nil
}

func (s *System) appendFailure(ctx context.Context, cause eventlog.Envelope, causeErr error) error {
	_, err := s.appendEvent(ctx, string(cause.StreamID), eventTypeSessionFailed, newFailedPayload(causeErr), string(cause.ID), string(cause.StreamID))
	return err
}
