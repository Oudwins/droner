package sessionevents

import (
	"encoding/json"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/eventlog"
	"github.com/Oudwins/droner/pkgs/droner/internals/remote"
)

const (
	taskIDPrefixCreate   = "session-create:"
	taskIDPrefixComplete = "session-complete:"
	taskIDPrefixDelete   = "session-delete:"
)

const (
	eventTypeSessionQueued                         = eventlog.EventType("session.queued")
	eventTypeSessionEnvironmentProvisioningStarted = eventlog.EventType("session.environment_provisioning.started")
	eventTypeSessionEnvironmentProvisioningSuccess = eventlog.EventType("session.environment_provisioning.success")
	eventTypeSessionEnvironmentProvisioningFailed  = eventlog.EventType("session.environment_provisioning.failed")
	eventTypeSessionRuntimeStarted                 = eventlog.EventType("session.runtime.started")
	eventTypeSessionReady                          = eventlog.EventType("session.ready")
	eventTypeSessionCompletionRequested            = eventlog.EventType("session.completion.requested")
	eventTypeSessionCompletionStarted              = eventlog.EventType("session.completion.started")
	eventTypeSessionCompletionSuccess              = eventlog.EventType("session.completion.success")
	eventTypeSessionCompletionFailed               = eventlog.EventType("session.completion.failed")
	eventTypeSessionDeletionRequested              = eventlog.EventType("session.deletion.requested")
	eventTypeSessionDeletionStarted                = eventlog.EventType("session.deletion.started")
	eventTypeSessionDeletionSuccess                = eventlog.EventType("session.deletion.success")
	eventTypeSessionDeletionFailed                 = eventlog.EventType("session.deletion.failed")
	eventTypeRemotePRClosed                        = eventlog.EventType("remote.pr.closed")
	eventTypeRemotePRMerged                        = eventlog.EventType("remote.pr.merged")
	eventTypeRemoteBranchDeleted                   = eventlog.EventType("remote.branch.deleted")
)

type queuedPayload struct {
	StreamID        string `json:"streamId"`
	SimpleID        string `json:"simpleId"`
	BackendID       string `json:"backendId"`
	RepoPath        string `json:"repoPath"`
	WorktreePath    string `json:"worktreePath"`
	RemoteURL       string `json:"remoteUrl,omitempty"`
	AgentConfigJSON string `json:"agentConfigJson,omitempty"`
}

type failedPayload struct {
	Error          string `json:"error"`
	BackendDetails string `json:"backendDetails,omitempty"`
}

type sessionIDPayload struct {
	SimpleID string `json:"simpleId"`
}

type remoteObservationPayload struct {
	SimpleID   string    `json:"simpleId"`
	RemoteURL  string    `json:"remoteUrl"`
	Branch     string    `json:"branch"`
	PRNumber   *int      `json:"prNumber,omitempty"`
	PRState    string    `json:"prState,omitempty"`
	ObservedAt time.Time `json:"observedAt"`
}

func newQueuedPayload(input CreateSessionInput) queuedPayload {
	return queuedPayload{
		StreamID:        input.StreamID,
		SimpleID:        input.SimpleID,
		BackendID:       input.BackendID.String(),
		RepoPath:        input.RepoPath,
		WorktreePath:    input.WorktreePath,
		RemoteURL:       input.RemoteURL,
		AgentConfigJSON: input.AgentConfigJSON,
	}
}

func newFailedPayload(err error) failedPayload {
	message := err.Error()
	return failedPayload{Error: message, BackendDetails: message}
}

func newSessionIDPayload(simpleID string) sessionIDPayload {
	return sessionIDPayload{SimpleID: simpleID}
}

func decodeQueuedPayload(evt eventlog.Envelope) (queuedPayload, error) {
	var payload queuedPayload
	err := json.Unmarshal(evt.Payload, &payload)
	return payload, err
}

func decodeFailedPayload(evt eventlog.Envelope) (failedPayload, error) {
	var payload failedPayload
	err := json.Unmarshal(evt.Payload, &payload)
	return payload, err
}

func decodeSessionIDPayload(evt eventlog.Envelope) (sessionIDPayload, error) {
	var payload sessionIDPayload
	err := json.Unmarshal(evt.Payload, &payload)
	return payload, err
}

func decodeRemoteObservationPayload(evt eventlog.Envelope) (remoteObservationPayload, error) {
	var payload remoteObservationPayload
	err := json.Unmarshal(evt.Payload, &payload)
	return payload, err
}

func newPendingEvent(streamID string, eventType eventlog.EventType, payload any, causationID, correlationID string) (eventlog.PendingEvent, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return eventlog.PendingEvent{}, err
	}
	return eventlog.PendingEvent{
		StreamID:      eventlog.StreamID(streamID),
		Type:          eventType,
		SchemaVersion: 1,
		Payload:       payloadBytes,
		CausationID:   eventlog.EventID(causationID),
		CorrelationID: correlationID,
	}, nil
}

func provisionStartedPayload(queued queuedPayload) map[string]string {
	return map[string]string{"simpleId": queued.SimpleID}
}

func readyStepPayload(queued queuedPayload) map[string]string {
	return map[string]string{"simpleId": queued.SimpleID}
}

func requestStepPayload(simpleID string) sessionIDPayload {
	return newSessionIDPayload(simpleID)
}

func queuedBackendID(payload queuedPayload) conf.BackendID {
	return conf.BackendID(payload.BackendID)
}

func newRemoteObservationPayload(simpleID string, event remote.BranchEvent) remoteObservationPayload {
	payload := remoteObservationPayload{
		SimpleID:   simpleID,
		RemoteURL:  event.RemoteURL,
		Branch:     event.Branch,
		ObservedAt: event.Timestamp,
	}
	if event.PRNumber != nil {
		payload.PRNumber = event.PRNumber
	}
	if event.PRState != nil {
		payload.PRState = *event.PRState
	}
	return payload
}

func remoteObservedEventType(eventType remote.BranchEventType) (eventlog.EventType, bool) {
	switch eventType {
	case remote.PRClosed:
		return eventTypeRemotePRClosed, true
	case remote.PRMerged:
		return eventTypeRemotePRMerged, true
	case remote.BranchDeleted:
		return eventTypeRemoteBranchDeleted, true
	default:
		return "", false
	}
}

func isRemoteObservedEventType(eventType eventlog.EventType) bool {
	switch eventType {
	case eventTypeRemotePRClosed, eventTypeRemotePRMerged, eventTypeRemoteBranchDeleted:
		return true
	default:
		return false
	}
}
