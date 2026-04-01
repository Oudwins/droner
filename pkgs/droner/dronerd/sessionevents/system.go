package sessionevents

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	coredb "github.com/Oudwins/droner/pkgs/droner/dronerd/db"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/internals/backends"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/sessionslog"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/eventlog"

	_ "modernc.org/sqlite"
)

const (
	consumerHydrationProcess   = "session_hydration_process"
	consumerProjection         = "session_projection"
	consumerCreateProcess      = "session_create_process"
	consumerCompleteProcess    = "session_complete_process"
	consumerDeleteProcess      = "session_delete_process"
	consumerRemoteSubscription = "session_remote_subscription"
	consumerRemoteObservation  = "session_remote_observation"
)

type System struct {
	db         *sql.DB
	queries    *coredb.Queries
	log        eventlog.EventLog
	logger     *slog.Logger
	config     *conf.Config
	backends   *backends.Store
	remoteSubs *remoteSubscriptionState

	startOnce sync.Once
}

type CreateSessionInput struct {
	StreamID        string
	SimpleID        string
	BackendID       conf.BackendID
	RepoPath        string
	WorktreePath    string
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
	SimpleID string
	State    string
}

type SessionRef struct {
	StreamID       string
	SimpleID       string
	BackendID      string
	RepoPath       string
	WorktreePath   string
	RemoteURL      string
	LifecycleState string
	PublicState    string
	LastError      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func Open(dataDir string, logger *slog.Logger, config *conf.Config, backendStore *backends.Store) (*System, error) {
	cleanDataDir := filepath.Clean(dataDir)
	dbPath := filepath.Join(cleanDataDir, "db", "droner.new.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, err
	}

	conn, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = conn.Close()
		}
	}()
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)
	if _, err := conn.Exec("PRAGMA journal_mode = WAL;"); err != nil {
		return nil, err
	}
	if _, err := conn.Exec("PRAGMA busy_timeout = 5000;"); err != nil {
		return nil, err
	}
	if _, err := conn.Exec("PRAGMA synchronous = NORMAL;"); err != nil {
		return nil, err
	}

	log, err := sessionslog.Open(cleanDataDir)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil && log != nil {
			_ = log.Close()
		}
	}()
	if err := coredb.ApplySchemas(conn); err != nil {
		return nil, err
	}

	return &System{db: conn, queries: coredb.New(conn), log: log, logger: logger, config: config, backends: backendStore, remoteSubs: newRemoteSubscriptionState()}, nil
}

func (s *System) Close() error {
	if s == nil {
		return nil
	}
	s.closeRemoteSubscriptions(context.Background())
	if s.log != nil {
		if err := s.log.Close(); err != nil {
			return err
		}
	}
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *System) Start(ctx context.Context) {
	if s == nil {
		return
	}
	s.startOnce.Do(func() {
		go s.enqueueHydrationRequests(ctx)
		go s.runSubscription(ctx, consumerProjection, eventlog.Subscription{
			ID: eventlog.SubscriberID(consumerProjection),
			Handle: func(ctx context.Context, evt eventlog.Envelope) error {
				return s.applyProjectionEvent(ctx, evt)
			},
		})
		go s.runSubscription(ctx, consumerCreateProcess, eventlog.Subscription{
			ID: eventlog.SubscriberID(consumerCreateProcess),
			Filter: func(evt eventlog.Envelope) bool {
				return evt.Type == eventTypeSessionQueued || evt.Type == eventTypeSessionEnvironmentProvisioningStarted
			},
			Handle: func(ctx context.Context, evt eventlog.Envelope) error {
				if evt.Type == eventTypeSessionQueued {
					return s.handleQueuedEvent(ctx, evt)
				}
				return s.handleProvisioningStarted(ctx, evt)
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
		go s.runSubscription(ctx, consumerRemoteSubscription, eventlog.Subscription{
			ID: eventlog.SubscriberID(consumerRemoteSubscription),
			Filter: func(evt eventlog.Envelope) bool {
				switch evt.Type {
				case eventTypeSessionReady, eventTypeSessionCompletionSuccess, eventTypeSessionDeletionSuccess:
					return true
				default:
					return false
				}
			},
			Handle: func(ctx context.Context, evt eventlog.Envelope) error {
				return s.handleRemoteSubscriptionEvent(ctx, evt)
			},
		})
		go s.runSubscription(ctx, consumerRemoteObservation, eventlog.Subscription{
			ID: eventlog.SubscriberID(consumerRemoteObservation),
			Filter: func(evt eventlog.Envelope) bool {
				return isRemoteObservedEventType(evt.Type)
			},
			Handle: func(ctx context.Context, evt eventlog.Envelope) error {
				return s.handleRemoteObservationEvent(ctx, evt)
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
		if _, err := s.appendEvent(ctx, ref.StreamID, eventTypeSessionHydrationRequested, requestStepPayload(ref.SimpleID), "", ref.StreamID); err != nil {
			s.logger.Error("failed to append session hydration request", "stream_id", ref.StreamID, "simple_id", ref.SimpleID, "error", err)
		}
	}
}

func (s *System) Hydrate(ctx context.Context) error {
	refs, err := s.listHydratableProjectionRefs(ctx)
	if err != nil {
		return err
	}
	for _, ref := range refs {
		if _, err := s.appendEvent(ctx, ref.StreamID, eventTypeSessionHydrationRequested, requestStepPayload(ref.SimpleID), "", ref.StreamID); err != nil {
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
	items := []ListItem{}
	if !all {
		rows, err := s.queries.ListVisibleSessionProjectionItems(ctx)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			items = append(items, ListItem{SimpleID: row.SimpleID, State: row.PublicState})
		}
		return items, nil
	}
	rows, err := s.queries.ListAllSessionProjectionItems(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		items = append(items, ListItem{SimpleID: row.SimpleID, State: row.PublicState})
	}
	return items, nil
}

func (s *System) LookupSessionBySimpleID(ctx context.Context, simpleID string) (SessionRef, error) {
	return s.loadProjectionBySimpleID(ctx, simpleID)
}

func (s *System) ListActiveSessionRefs(ctx context.Context) ([]SessionRef, error) {
	return s.listActiveProjectionRefs(ctx)
}

func (s *System) RequestCompletion(ctx context.Context, simpleID string) (OperationResult, error) {
	ref, err := s.LookupSessionBySimpleID(ctx, simpleID)
	if err != nil {
		return OperationResult{}, err
	}
	if ref.LifecycleState == string(eventTypeSessionCompletionSuccess) || ref.LifecycleState == string(eventTypeSessionDeletionSuccess) {
		return OperationResult{TaskID: taskIDPrefixComplete + ref.StreamID}, nil
	}
	if _, err := s.appendEvent(ctx, ref.StreamID, eventTypeSessionCompletionRequested, requestStepPayload(ref.SimpleID), "", ref.StreamID); err != nil {
		return OperationResult{}, err
	}
	return OperationResult{TaskID: taskIDPrefixComplete + ref.StreamID}, nil
}

func (s *System) RequestDeletion(ctx context.Context, simpleID string) (OperationResult, error) {
	ref, err := s.LookupSessionBySimpleID(ctx, simpleID)
	if err != nil {
		return OperationResult{}, err
	}
	if ref.LifecycleState == string(eventTypeSessionDeletionStarted) || ref.LifecycleState == string(eventTypeSessionDeletionSuccess) {
		return OperationResult{TaskID: taskIDPrefixDelete + ref.StreamID}, nil
	}
	if _, err := s.appendEvent(ctx, ref.StreamID, eventTypeSessionDeletionRequested, requestStepPayload(ref.SimpleID), "", ref.StreamID); err != nil {
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
		if ref.LifecycleState == string(eventTypeSessionDeletionRequested) || ref.LifecycleState == string(eventTypeSessionDeletionStarted) || ref.LifecycleState == string(eventTypeSessionDeletionSuccess) {
			continue
		}
		if _, err := s.appendEvent(ctx, ref.StreamID, eventTypeSessionDeletionRequested, requestStepPayload(ref.SimpleID), "", ref.StreamID); err != nil {
			return NukeResult{}, err
		}
		requested++
	}
	return NukeResult{Requested: requested}, nil
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
