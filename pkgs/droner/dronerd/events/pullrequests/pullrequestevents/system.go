package pullrequestevents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/internals/remote"
	"github.com/Oudwins/droner/pkgs/droner/internals/eventlog"
)

const consumerPullRequestSessionSubscription = "pullrequest_session_subscription"

var subscribeRemoteBranchEvents = remote.SubscribeBranchEvents
var unsubscribeRemoteBranchEvents = remote.UnsubscribeBranchEvents

type System struct {
	prLog      eventlog.EventLog
	sessionLog eventlog.EventLog
	sessions   SessionLookupStore
	snapshots  PullRequestSnapshotStore
	logger     *slog.Logger
	remoteSubs *subscriptionState
	startOnce  sync.Once
}

type subscriptionState struct {
	mu   sync.Mutex
	subs map[string]string
}

func New(prLog eventlog.EventLog, sessionLog eventlog.EventLog, sessions SessionLookupStore, snapshots PullRequestSnapshotStore, logger *slog.Logger) *System {
	return &System{prLog: prLog, sessionLog: sessionLog, sessions: sessions, snapshots: snapshots, logger: logger, remoteSubs: &subscriptionState{subs: map[string]string{}}}
}

func (s *System) Start(ctx context.Context) {
	if s == nil {
		return
	}
	s.startOnce.Do(func() {
		go s.runSubscription(ctx)
	})
}

func (s *System) Close() error {
	if s == nil {
		return nil
	}
	s.closeRemoteSubscriptions(context.Background())
	return nil
}

func (s *System) runSubscription(ctx context.Context) {
	sub := eventlog.Subscription{
		ID: eventlog.SubscriberID(consumerPullRequestSessionSubscription),
		Filter: func(evt eventlog.Envelope) bool {
			switch evt.Type {
			case eventTypeSessionReady, eventTypeSessionCompletionSuccess, eventTypeSessionDeletionSuccess:
				return true
			default:
				return false
			}
		},
		Handle: s.handleSessionEvent,
	}
	for {
		if err := s.sessionLog.Subscribe(ctx, sub); err != nil && !errors.Is(err, context.Canceled) {
			s.logger.Error("pullrequestevents subscription failed", "error", err)
		} else {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (s *System) handleSessionEvent(ctx context.Context, evt eventlog.Envelope) error {
	ref, err := s.sessions.LoadByStreamID(ctx, string(evt.StreamID))
	if err != nil {
		return err
	}
	remoteURL := strings.TrimSpace(ref.RemoteURL)
	branch := strings.TrimSpace(ref.Branch)
	switch evt.Type {
	case eventTypeSessionReady:
		return s.ensureRemoteSubscription(ctx, string(evt.StreamID), remoteURL, branch)
	case eventTypeSessionCompletionSuccess, eventTypeSessionDeletionSuccess:
		return s.stopRemoteSubscription(ctx, remoteURL, branch)
	default:
		return nil
	}
}

func (s *System) ensureRemoteSubscription(ctx context.Context, sessionStreamID, remoteURL, branch string) error {
	if remoteURL == "" || branch == "" {
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
		if err := s.handleRemoteEvent(appendCtx, sessionStreamID, event); err != nil {
			s.logger.Error("failed to ingest pull request event", "stream_id", sessionStreamID, "event_type", event.Type, "error", err)
		}
	}
	if err := subscribeRemoteBranchEvents(ctx, remoteURL, branch, handler); err != nil {
		return err
	}
	s.remoteSubs.mu.Lock()
	s.remoteSubs.subs[key] = sessionStreamID
	s.remoteSubs.mu.Unlock()
	return nil
}

func (s *System) stopRemoteSubscription(ctx context.Context, remoteURL, branch string) error {
	if remoteURL == "" || branch == "" {
		return nil
	}
	key := remoteSubscriptionKey(remoteURL, branch)
	s.remoteSubs.mu.Lock()
	if _, exists := s.remoteSubs.subs[key]; !exists {
		s.remoteSubs.mu.Unlock()
		return nil
	}
	delete(s.remoteSubs.subs, key)
	s.remoteSubs.mu.Unlock()
	if err := unsubscribeRemoteBranchEvents(ctx, remoteURL, branch); err != nil && !errors.Is(err, remote.ErrUnsupportedRemote) {
		return err
	}
	return nil
}

func (s *System) closeRemoteSubscriptions(ctx context.Context) {
	s.remoteSubs.mu.Lock()
	keys := make([]string, 0, len(s.remoteSubs.subs))
	for key := range s.remoteSubs.subs {
		keys = append(keys, key)
	}
	s.remoteSubs.mu.Unlock()
	for _, key := range keys {
		remoteURL, branch, ok := splitRemoteSubscriptionKey(key)
		if ok {
			_ = s.stopRemoteSubscription(ctx, remoteURL, branch)
		}
	}
}

func (s *System) handleRemoteEvent(ctx context.Context, sessionStreamID string, event remote.BranchEvent) error {
	switch event.Type {
	case remote.PRObserved:
		if event.PRSnapshot == nil {
			return nil
		}
		return s.ingestObserved(ctx, sessionStreamID, *event.PRSnapshot, event.Timestamp)
	case remote.PRClosed, remote.PRMerged:
		return s.appendTerminalPREvent(ctx, event)
	default:
		return nil
	}
}

func (s *System) ingestObserved(ctx context.Context, sessionStreamID string, snapshot remote.PullRequestSnapshot, observedAt time.Time) error {
	streamID := prStreamID(snapshot)
	oldSnapshot, found, err := s.loadStoredSnapshot(ctx, streamID)
	if err != nil {
		return err
	}
	changes := []PRFieldChange{}
	kind := "initial"
	if found {
		changes = diffPullRequestSnapshots(oldSnapshot, snapshot)
		if len(changes) == 0 {
			return nil
		}
		kind = "delta"
	}
	if err := s.snapshots.Upsert(ctx, snapshot, observedAt); err != nil {
		return err
	}
	payload := prObservedPayload{Provider: snapshot.Provider, StreamID: streamID, RemoteURL: snapshot.RemoteURL, RepoOwner: snapshot.RepoOwner, RepoName: snapshot.RepoName, Number: snapshot.Number, ObservedAt: observedAt.UTC(), Kind: kind, Changes: changes}
	if !found {
		payload.Snapshot = &snapshot
	}
	if err := s.appendPRLog(ctx, streamID, eventTypePRObserved, payload, "", streamID); err != nil {
		return err
	}
	return s.appendSessionSummaryEvents(ctx, sessionStreamID, streamID, oldSnapshot, snapshot, found, observedAt)
}

func (s *System) appendTerminalPREvent(ctx context.Context, event remote.BranchEvent) error {
	if event.PRNumber == nil {
		return nil
	}
	streamID := fmt.Sprintf("github:%s#%d", strings.TrimSuffix(strings.TrimPrefix(event.RemoteURL, "git@github.com:"), ".git"), *event.PRNumber)
	payload := map[string]any{"remoteUrl": event.RemoteURL, "branch": event.Branch, "prNumber": *event.PRNumber, "observedAt": event.Timestamp.UTC()}
	eventType := eventTypePRClosed
	if event.Type == remote.PRMerged {
		eventType = eventTypePRMerged
	}
	return s.appendPRLog(ctx, streamID, eventType, payload, "", streamID)
}

func (s *System) appendSessionSummaryEvents(ctx context.Context, sessionStreamID, prStreamID string, oldSnapshot, newSnapshot remote.PullRequestSnapshot, found bool, observedAt time.Time) error {
	pending := []eventlog.PendingEvent{}
	if !found {
		evt, err := marshalPendingEvent(sessionStreamID, eventTypeSessionPRLinked, sessionPRLinkedPayload{PRStreamID: prStreamID, PRNumber: newSnapshot.Number, State: sessionPRState(newSnapshot), CIState: newSnapshot.CI.State, LinkedAt: observedAt.UTC()}, "", sessionStreamID)
		if err != nil {
			return err
		}
		pending = append(pending, evt)
	}
	oldState := sessionPRState(oldSnapshot)
	newState := sessionPRState(newSnapshot)
	if !found || oldState != newState {
		eventType := eventTypeSessionPRStateChanged
		if newState == "closed" {
			eventType = eventTypeSessionPRClosed
		}
		if newState == "merged" {
			eventType = eventTypeSessionPRMerged
		}
		evt, err := marshalPendingEvent(sessionStreamID, eventType, sessionPRStateChangedPayload{PRStreamID: prStreamID, PRNumber: newSnapshot.Number, State: newState, ChangedAt: observedAt.UTC()}, "", sessionStreamID)
		if err != nil {
			return err
		}
		pending = append(pending, evt)
	}
	if !found || oldSnapshot.CI.State != newSnapshot.CI.State {
		evt, err := marshalPendingEvent(sessionStreamID, eventTypeSessionPRCIStateChanged, sessionPRCIStateChangedPayload{PRStreamID: prStreamID, PRNumber: newSnapshot.Number, CIState: newSnapshot.CI.State, ChangedAt: observedAt.UTC()}, "", sessionStreamID)
		if err != nil {
			return err
		}
		pending = append(pending, evt)
	}
	for _, evt := range pending {
		if _, err := s.sessionLog.Append(ctx, evt); err != nil {
			return err
		}
	}
	return nil
}

func (s *System) loadStoredSnapshot(ctx context.Context, streamID string) (remote.PullRequestSnapshot, bool, error) {
	return s.snapshots.Load(ctx, streamID)
}

func (s *System) appendPRLog(ctx context.Context, streamID string, eventType eventlog.EventType, payload any, causationID, correlationID string) error {
	pending, err := marshalPendingEvent(streamID, eventType, payload, causationID, correlationID)
	if err != nil {
		return err
	}
	_, err = s.prLog.Append(ctx, pending)
	return err
}

func prStreamID(snapshot remote.PullRequestSnapshot) string {
	return fmt.Sprintf("%s:%s/%s#%d", snapshot.Provider, snapshot.RepoOwner, snapshot.RepoName, snapshot.Number)
}

func stableSnapshotJSON(snapshot remote.PullRequestSnapshot) ([]byte, error) {
	return json.Marshal(snapshot)
}

func diffPullRequestSnapshots(oldSnapshot, newSnapshot remote.PullRequestSnapshot) []PRFieldChange {
	changes := []PRFieldChange{}
	appendChange := func(field string, oldValue, newValue any) {
		if !reflect.DeepEqual(oldValue, newValue) {
			changes = append(changes, PRFieldChange{Field: field, Old: oldValue, New: newValue})
		}
	}
	appendChange("state", oldSnapshot.State, newSnapshot.State)
	appendChange("draft", oldSnapshot.Draft, newSnapshot.Draft)
	appendChange("title", oldSnapshot.Title, newSnapshot.Title)
	appendChange("head_ref", oldSnapshot.HeadRef, newSnapshot.HeadRef)
	appendChange("head_sha", oldSnapshot.HeadSHA, newSnapshot.HeadSHA)
	appendChange("base_ref", oldSnapshot.BaseRef, newSnapshot.BaseRef)
	appendChange("mergeable", oldSnapshot.Mergeable, newSnapshot.Mergeable)
	appendChange("mergeable_state", oldSnapshot.MergeableState, newSnapshot.MergeableState)
	appendChange("requested_reviewers", oldSnapshot.RequestedReviewers, newSnapshot.RequestedReviewers)
	appendChange("requested_teams", oldSnapshot.RequestedTeams, newSnapshot.RequestedTeams)
	appendChange("review_summary", oldSnapshot.ReviewSummary, newSnapshot.ReviewSummary)
	appendChange("ci", oldSnapshot.CI, newSnapshot.CI)
	appendChange("closed_at", oldSnapshot.ClosedAt, newSnapshot.ClosedAt)
	appendChange("merged_at", oldSnapshot.MergedAt, newSnapshot.MergedAt)
	return changes
}

func sessionPRState(snapshot remote.PullRequestSnapshot) string {
	if snapshot.MergedAt != nil {
		return "merged"
	}
	if snapshot.State == "closed" || snapshot.ClosedAt != nil {
		return "closed"
	}
	if len(snapshot.ReviewSummary.ChangesRequested) > 0 {
		return "changes_requested"
	}
	if len(snapshot.ReviewSummary.Approved) > 0 {
		return "approved"
	}
	if len(snapshot.RequestedReviewers) > 0 || len(snapshot.RequestedTeams) > 0 {
		return "in_review"
	}
	return "open"
}

func remoteSubscriptionKey(remoteURL, branch string) string {
	return strings.TrimSpace(remoteURL) + "\x00" + strings.TrimSpace(branch)
}

func splitRemoteSubscriptionKey(key string) (string, string, bool) {
	remoteURL, branch, ok := strings.Cut(key, "\x00")
	return remoteURL, branch, ok
}
