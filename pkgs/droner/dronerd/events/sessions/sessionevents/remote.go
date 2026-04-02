package sessionevents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/internals/remote"
	"github.com/Oudwins/droner/pkgs/droner/internals/eventlog"
)

var subscribeRemoteBranchEvents = remote.SubscribeBranchEvents
var unsubscribeRemoteBranchEvents = remote.UnsubscribeBranchEvents

type remoteSubscriptionState struct {
	mu   sync.Mutex
	subs map[string]struct{}
}

func newRemoteSubscriptionState() *remoteSubscriptionState {
	return &remoteSubscriptionState{subs: map[string]struct{}{}}
}

func remoteSubscriptionKey(remoteURL, branch string) string {
	return strings.TrimSpace(remoteURL) + "\x00" + strings.TrimSpace(branch)
}

func splitRemoteSubscriptionKey(key string) (string, string, bool) {
	remoteURL, branch, ok := strings.Cut(key, "\x00")
	if !ok {
		return "", "", false
	}
	return remoteURL, branch, true
}

func (s *System) ensureRemoteSubscription(ctx context.Context, state sessionState) error {
	remoteURL := strings.TrimSpace(state.RemoteURL)
	if remoteURL == "" {
		return nil
	}
	branch := strings.TrimSpace(state.Branch)
	if branch == "" {
		return nil
	}

	key := remoteSubscriptionKey(remoteURL, branch)
	s.remoteSubs.mu.Lock()
	if _, exists := s.remoteSubs.subs[key]; exists {
		s.remoteSubs.mu.Unlock()
		return nil
	}
	s.remoteSubs.mu.Unlock()

	handler := func(event remote.BranchEvent) {
		appendCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.appendRemoteObservation(appendCtx, state.StreamID, state.Branch, event); err != nil {
			s.logger.Error("failed to append remote observation", "stream_id", state.StreamID, "branch", state.Branch, "event_type", event.Type, "error", err)
		}
	}

	if err := subscribeRemoteBranchEvents(ctx, remoteURL, branch, handler); err != nil {
		return err
	}

	s.remoteSubs.mu.Lock()
	s.remoteSubs.subs[key] = struct{}{}
	s.remoteSubs.mu.Unlock()

	s.logger.Info("remote subscription started", "stream_id", state.StreamID, "branch", state.Branch, "remote_url", remoteURL)
	return nil
}

func (s *System) stopRemoteSubscription(ctx context.Context, remoteURL, branch string) error {
	remoteURL = strings.TrimSpace(remoteURL)
	branch = strings.TrimSpace(branch)
	if remoteURL == "" || branch == "" {
		return nil
	}

	key := remoteSubscriptionKey(remoteURL, branch)
	s.remoteSubs.mu.Lock()
	if _, exists := s.remoteSubs.subs[key]; !exists {
		s.remoteSubs.mu.Unlock()
		return nil
	}
	s.remoteSubs.mu.Unlock()

	if err := unsubscribeRemoteBranchEvents(ctx, remoteURL, branch); err != nil && !errors.Is(err, remote.ErrUnsupportedRemote) {
		return err
	}

	s.remoteSubs.mu.Lock()
	delete(s.remoteSubs.subs, key)
	s.remoteSubs.mu.Unlock()

	s.logger.Info("remote subscription stopped", "branch", branch, "remote_url", remoteURL)
	return nil
}

func (s *System) closeRemoteSubscriptions(ctx context.Context) {
	if s == nil || s.remoteSubs == nil {
		return
	}
	s.remoteSubs.mu.Lock()
	keys := make([]string, 0, len(s.remoteSubs.subs))
	for key := range s.remoteSubs.subs {
		keys = append(keys, key)
	}
	s.remoteSubs.mu.Unlock()

	for _, key := range keys {
		remoteURL, branch, ok := splitRemoteSubscriptionKey(key)
		if !ok {
			continue
		}
		if err := s.stopRemoteSubscription(ctx, remoteURL, branch); err != nil {
			s.logger.Error("failed to stop remote subscription during close", "remote_url", remoteURL, "branch", branch, "error", err)
		}
	}
}

func (s *System) appendRemoteObservation(ctx context.Context, streamID, branch string, event remote.BranchEvent) error {
	eventType, ok := remoteObservedEventType(event.Type)
	if !ok {
		return nil
	}
	_, err := s.appendEvent(ctx, streamID, eventType, newRemoteObservationPayload(branch, event), "", streamID)
	return err
}

func (s *System) handleRemoteSubscriptionEvent(ctx context.Context, evt eventlog.Envelope) error {
	state, err := s.loadSessionState(ctx, string(evt.StreamID))
	if err != nil {
		return err
	}

	switch evt.Type {
	case eventTypeSessionReady:
		return s.ensureRemoteSubscription(ctx, state)
	case eventTypeSessionCompletionSuccess, eventTypeSessionDeletionSuccess:
		return s.stopRemoteSubscription(ctx, state.RemoteURL, state.Branch)
	default:
		return nil
	}
}

func (s *System) handleRemoteObservationEvent(ctx context.Context, evt eventlog.Envelope) error {
	if !isRemoteObservedEventType(evt.Type) {
		return nil
	}

	state, err := s.loadSessionState(ctx, string(evt.StreamID))
	if err != nil {
		return err
	}

	switch state.LifecycleState {
	case string(eventTypeSessionCompletionRequested), string(eventTypeSessionCompletionStarted), string(eventTypeSessionCompletionSuccess), string(eventTypeSessionCompletionFailed), string(eventTypeSessionDeletionRequested), string(eventTypeSessionDeletionStarted), string(eventTypeSessionDeletionSuccess), string(eventTypeSessionDeletionFailed):
		return nil
	}

	payload, err := decodeRemoteObservationPayload(evt)
	if err != nil {
		return err
	}
	if strings.TrimSpace(payload.Branch) == "" {
		return fmt.Errorf("remote observation missing branch")
	}

	_, err = s.appendEvent(ctx, string(evt.StreamID), eventTypeSessionCompletionRequested, requestStepPayload(payload.Branch), string(evt.ID), string(evt.StreamID))
	return err
}
