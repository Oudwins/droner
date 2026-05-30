package sessionevents

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/events/sessions/agentevents"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/internals/backends"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/eventlog"
)

const (
	consumerHydrationProcess = "session_hydration_process"
	consumerProjection       = "session_projection"
	consumerCreateProcess    = "session_create_process"
	consumerCompleteProcess  = "session_complete_process"
	consumerDeleteProcess    = "session_delete_process"
	listDirectionBefore      = "before"
	listDirectionAfter       = "after"
)

type System struct {
	log            eventlog.EventLog
	projections    ProjectionStore
	resetter       SessionResetter
	logger         *slog.Logger
	config         *conf.Config
	backends       *backends.Store
	runAgentEvents func(context.Context, *slog.Logger, conf.OpenCodeConfig, func(context.Context, agentevents.Event) error) error

	startOnce sync.Once
}

type SessionResetter interface {
	ResetStreamToEvent(ctx context.Context, streamID eventlog.StreamID, eventID eventlog.EventID) (eventlog.Envelope, error)
}

type CreateSessionInput struct {
	StreamID        string
	Harness         conf.HarnessID
	RequestedBranch string
	BackendID       conf.BackendID
	RepoPath        string
	RemoteURL       string
	AgentConfigJSON string
}

type CreateSessionResult struct {
	TaskID string
}

type OperationResult struct {
	TaskID string
}

type NukeResult struct {
	Requested int
}

type ListItem struct {
	ID        string
	Repo      string
	RemoteURL string
	Branch    string
	State     PublicState
}

type SessionRef struct {
	StreamID       string
	Harness        string
	Branch         string
	BackendID      string
	RepoPath       string
	WorktreePath   string
	RemoteURL      string
	LifecycleState LifecycleState
	PublicState    PublicState
	LastError      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func New(log eventlog.EventLog, projections ProjectionStore, resetter SessionResetter, logger *slog.Logger, config *conf.Config, backendStore *backends.Store) *System {
	return &System{log: log, projections: projections, resetter: resetter, logger: logger, config: config, backends: backendStore, runAgentEvents: agentevents.RunOpenCode}
}

func (s *System) Close() error {
	return nil
}

func (s *System) Start(ctx context.Context) {
	if s == nil {
		return
	}
	s.startOnce.Do(func() {
		go s.enqueueHydrationRequests(ctx)
		go s.runAgentEventBridge(ctx)
		go s.runSubscription(ctx, consumerProjection, eventlog.Subscription{
			ID: eventlog.SubscriberID(consumerProjection),
			Handle: func(ctx context.Context, evt eventlog.Envelope) error {
				return s.applyProjectionEvent(ctx, evt)
			},
		})
		go s.runSubscription(ctx, consumerCreateProcess, eventlog.Subscription{
			ID: eventlog.SubscriberID(consumerCreateProcess),
			Filter: func(evt eventlog.Envelope) bool {
				switch evt.Type {
				case eventTypeSessionQueued, eventTypeSessionEnrichmentRequested, eventTypeSessionEnrichmentSucceeded, eventTypeSessionEnvironmentProvisioningStarted:
					return true
				default:
					return false
				}
			},
			Handle: func(ctx context.Context, evt eventlog.Envelope) error {
				switch evt.Type {
				case eventTypeSessionQueued:
					return s.handleQueuedEvent(ctx, evt)
				case eventTypeSessionEnrichmentRequested:
					return s.handleEnrichmentRequested(ctx, evt)
				case eventTypeSessionEnrichmentSucceeded:
					return s.handleEnrichmentSucceeded(ctx, evt)
				default:
					return s.handleProvisioningStarted(ctx, evt)
				}
			},
		})
		go s.runSubscription(ctx, consumerHydrationProcess, eventlog.Subscription{
			ID: eventlog.SubscriberID(consumerHydrationProcess),
			Filter: func(evt eventlog.Envelope) bool {
				return evt.Type == eventTypeSessionHydrationRequested
			},
			Handle: func(ctx context.Context, evt eventlog.Envelope) error {
				return s.handleHydrationRequested(ctx, evt)
			},
		})
		go s.runSubscription(ctx, consumerCompleteProcess, eventlog.Subscription{
			ID: eventlog.SubscriberID(consumerCompleteProcess),
			Filter: func(evt eventlog.Envelope) bool {
				return evt.Type == eventTypeSessionCompletionRequested || evt.Type == eventTypeSessionCompletionStarted
			},
			Handle: func(ctx context.Context, evt eventlog.Envelope) error {
				if evt.Type == eventTypeSessionCompletionRequested {
					return s.handleCompletionRequested(ctx, evt)
				}
				return s.handleCompletionStarted(ctx, evt)
			},
		})
		go s.runSubscription(ctx, consumerDeleteProcess, eventlog.Subscription{
			ID: eventlog.SubscriberID(consumerDeleteProcess),
			Filter: func(evt eventlog.Envelope) bool {
				return evt.Type == eventTypeSessionDeletionRequested || evt.Type == eventTypeSessionDeletionStarted
			},
			Handle: func(ctx context.Context, evt eventlog.Envelope) error {
				if evt.Type == eventTypeSessionDeletionRequested {
					return s.handleDeletionRequested(ctx, evt)
				}
				return s.handleDeletionStarted(ctx, evt)
			},
		})
	})
}

func (s *System) enqueueHydrationRequests(ctx context.Context) {
	refs, err := s.listHydratableProjectionRefs(ctx)
	if err != nil {
		s.logger.Error("failed to list hydratable sessions", "error", err)
		return
	}
	for _, ref := range refs {
		if _, err := s.appendEvent(ctx, ref.StreamID, eventTypeSessionHydrationRequested, requestStepPayload(ref.Branch), "", ref.StreamID); err != nil {
			s.logger.Error("failed to append session hydration request", "stream_id", ref.StreamID, "branch", ref.Branch, "error", err)
		}
	}
}

func (s *System) Hydrate(ctx context.Context) error {
	refs, err := s.listHydratableProjectionRefs(ctx)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		if _, err := s.appendEvent(ctx, ref.StreamID, eventTypeSessionHydrationRequested, requestStepPayload(ref.Branch), "", ref.StreamID); err != nil {
			return err
		}
	}
	return nil
}

func (s *System) CreateSession(ctx context.Context, input CreateSessionInput) (CreateSessionResult, error) {
	if _, err := s.appendEvent(ctx, input.StreamID, eventTypeSessionQueued, newQueuedPayload(input), "", input.StreamID); err != nil {
		return CreateSessionResult{}, err
	}
	return CreateSessionResult{TaskID: taskIDPrefixCreate + input.StreamID}, nil
}

func (s *System) ListSessions(ctx context.Context, all bool) ([]ListItem, error) {
	if !all {
		return s.projections.ListVisible(ctx)
	}
	return s.projections.ListAll(ctx)
}

// ListSessionProjections reads directly from the session_projection table.
// Cursor pagination is relative to the current newest-first ordering:
// "after" returns older rows that appear after the anchor, while "before"
// returns newer rows that appear before it.
func (s *System) ListSessionProjections(ctx context.Context, statuses []string, limit int, cursor string, direction string) ([]ListItem, error) {
	statusesArg, statusesValue := statusesValue(statuses)

	if strings.TrimSpace(direction) == "" {
		direction = listDirectionAfter
	}

	var (
		items []ListItem
		err   error
	)

	switch {
	case cursor == "":
		items, err = s.listSessionProjectionItemsAfterCursor(ctx, statusesArg, statusesValue, "", limit)
	case direction == listDirectionAfter:
		items, err = s.listSessionProjectionItemsAfterCursor(ctx, statusesArg, statusesValue, cursor, limit)
	case direction == listDirectionBefore:
		items, err = s.listSessionProjectionItemsBeforeCursor(ctx, statusesArg, statusesValue, cursor, limit)
	default:
		return nil, fmt.Errorf("invalid list direction %q", direction)
	}
	if err != nil {
		return nil, err
	}
	if cursor != "" && direction == listDirectionBefore {
		reverseListItems(items)
	}
	return items, nil
}

func (s *System) ListOldestSessionProjections(ctx context.Context, statuses []string, limit int) ([]ListItem, error) {
	statusesArg, statusesValue := statusesValue(statuses)

	return s.listSessionProjectionItemsOldest(ctx, statusesArg, statusesValue, limit)
}

func (s *System) listSessionProjectionItemsAfterCursor(ctx context.Context, statusesArg string, statusesValue sql.NullString, cursor string, limit int) ([]ListItem, error) {
	return s.projections.ListAfterCursor(ctx, statusesArg, statusesValue, cursor, limit)
}

func (s *System) listSessionProjectionItemsBeforeCursor(ctx context.Context, statusesArg string, statusesValue sql.NullString, cursor string, limit int) ([]ListItem, error) {
	return s.projections.ListBeforeCursor(ctx, statusesArg, statusesValue, cursor, limit)
}

func (s *System) listSessionProjectionItemsOldest(ctx context.Context, statusesArg string, statusesValue sql.NullString, limit int) ([]ListItem, error) {
	return s.projections.ListOldest(ctx, statusesArg, statusesValue, limit)
}

func reverseListItems(items []ListItem) {
	for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
		items[left], items[right] = items[right], items[left]
	}
}

func (s *System) LookupSessionByBranch(ctx context.Context, branch string) (SessionRef, error) {
	return s.loadCurrentProjectionByBranch(ctx, branch)
}

func (s *System) LookupBlockedSessionByRepoAndBranch(ctx context.Context, repoPath string, branch string) (SessionRef, error) {
	return s.loadBlockedProjectionByRepoAndBranch(ctx, repoPath, branch)
}

func (s *System) LookupSessionByWorktreePath(ctx context.Context, worktreePath string) (SessionRef, error) {
	return s.loadProjectionByWorktreePath(ctx, worktreePath)
}

func (s *System) LookupLatestNavigationSessionByBranch(ctx context.Context, branch string) (SessionRef, error) {
	return s.loadLatestNavigationProjectionByBranch(ctx, branch)
}

func (s *System) ListActiveSessionRefs(ctx context.Context) ([]SessionRef, error) {
	return s.listActiveProjectionRefs(ctx)
}

func (s *System) RequestCompletion(ctx context.Context, branch string) (OperationResult, error) {
	ref, err := s.LookupSessionByBranch(ctx, branch)
	if err != nil {
		return OperationResult{}, err
	}
	if ref.LifecycleState == LifecycleStateCompletionSuccess || ref.LifecycleState == LifecycleStateDeletionSuccess {
		return OperationResult{TaskID: taskIDPrefixComplete + ref.StreamID}, nil
	}
	if _, err := s.appendEvent(ctx, ref.StreamID, eventTypeSessionCompletionRequested, requestStepPayload(ref.Branch), "", ref.StreamID); err != nil {
		return OperationResult{}, err
	}
	return OperationResult{TaskID: taskIDPrefixComplete + ref.StreamID}, nil
}

func (s *System) RequestDeletion(ctx context.Context, branch string) (OperationResult, error) {
	ref, err := s.LookupSessionByBranch(ctx, branch)
	if err != nil {
		return OperationResult{}, err
	}
	if ref.LifecycleState == LifecycleStateDeletionStarted || ref.LifecycleState == LifecycleStateDeletionSuccess {
		return OperationResult{TaskID: taskIDPrefixDelete + ref.StreamID}, nil
	}
	if _, err := s.appendEvent(ctx, ref.StreamID, eventTypeSessionDeletionRequested, requestStepPayload(ref.Branch), "", ref.StreamID); err != nil {
		return OperationResult{}, err
	}
	return OperationResult{TaskID: taskIDPrefixDelete + ref.StreamID}, nil
}

func (s *System) NukeSessions(ctx context.Context) (NukeResult, error) {
	refs, err := s.ListActiveSessionRefs(ctx)
	if err != nil {
		return NukeResult{}, err
	}
	requested := 0
	for _, ref := range refs {
		if ref.LifecycleState == LifecycleStateDeletionRequested || ref.LifecycleState == LifecycleStateDeletionStarted || ref.LifecycleState == LifecycleStateDeletionSuccess {
			continue
		}
		if _, err := s.appendEvent(ctx, ref.StreamID, eventTypeSessionDeletionRequested, requestStepPayload(ref.Branch), "", ref.StreamID); err != nil {
			return NukeResult{}, err
		}
		requested++
	}
	return NukeResult{Requested: requested}, nil
}

func (s *System) ResetToEvent(ctx context.Context, streamID string, eventID string) (OperationResult, error) {
	streamID = strings.TrimSpace(streamID)
	eventID = strings.TrimSpace(eventID)
	if streamID == "" || eventID == "" {
		return OperationResult{}, sql.ErrNoRows
	}
	if s.resetter == nil {
		return OperationResult{}, errors.New("sessions backend unavailable")
	}

	if err := s.projections.Delete(ctx, streamID); err != nil {
		return OperationResult{}, err
	}
	if _, err := s.resetter.ResetStreamToEvent(ctx, eventlog.StreamID(streamID), eventlog.EventID(eventID)); err != nil {
		if rebuildErr := s.rebuildProjection(ctx, streamID); rebuildErr != nil {
			s.logger.Error("failed to rebuild session projection after reset error", "stream_id", streamID, "error", rebuildErr)
		}
		return OperationResult{}, err
	}

	return OperationResult{TaskID: taskIDPrefixReset + streamID}, nil
}

func newListItem(id, repoPath, remoteURL, branch string, state PublicState) ListItem {
	repo := filepath.Base(filepath.Clean(repoPath))
	if repo == "." || repo == string(filepath.Separator) {
		repo = ""
	}
	return ListItem{ID: id, Repo: repo, RemoteURL: remoteURL, Branch: branch, State: state}
}

func (s *System) rebuildProjection(ctx context.Context, streamID string) error {
	state, err := s.loadSessionState(ctx, streamID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.upsertProjection(ctx, state.projectionMutation())
}

func (s *System) runAgentEventBridge(ctx context.Context) {
	if s == nil || s.runAgentEvents == nil || s.config == nil {
		return
	}
	if err := s.runAgentEvents(ctx, s.logger, s.config.Sessions.Harness.Providers.OpenCode, s.handleAgentEvent); err != nil && !errors.Is(err, context.Canceled) {
		s.logger.Error("agent event bridge stopped", "error", err)
	}
}

func (s *System) handleAgentEvent(ctx context.Context, evt agentevents.Event) error {
	ref, err := s.loadProjectionByWorktreePath(ctx, evt.WorktreePath)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}
	if ref.Harness != conf.HarnessOpenCode.String() || !ref.LifecycleState.AllowsAgentRuntime() || ref.PublicState.IsTerminal() {
		return nil
	}

	var (
		eventType   eventlog.EventType
		publicState PublicState
	)
	switch evt.State {
	case agentevents.StateBusy:
		eventType = eventTypeSessionAgentBusy
		publicState = PublicStateActiveBusy
	case agentevents.StateIdle:
		eventType = eventTypeSessionAgentIdle
		publicState = PublicStateActiveIdle
	default:
		return nil
	}
	if ref.PublicState == publicState {
		return nil
	}
	_, err = s.appendEvent(ctx, ref.StreamID, eventType, requestStepPayload(ref.Branch), "", ref.StreamID)
	return err
}

func (s *System) runSubscription(ctx context.Context, consumerName string, sub eventlog.Subscription) {
	for {
		if err := s.log.Subscribe(ctx, sub); err != nil && !errors.Is(err, context.Canceled) {
			s.logger.Error("sessionevents subscription failed", "consumer", consumerName, "error", err)
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

func (s *System) appendEvent(ctx context.Context, streamID string, eventType eventlog.EventType, payload any, causationID, correlationID string) (eventlog.Envelope, error) {
	pending, err := newPendingEvent(streamID, eventType, payload, causationID, correlationID)
	if err != nil {
		return eventlog.Envelope{}, err
	}
	return s.log.Append(ctx, pending)
}
