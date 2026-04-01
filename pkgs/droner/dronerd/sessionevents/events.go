package sessionevents

import (
	"encoding/json"

	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/eventlog"
)

const (
	taskIDPrefix = "session-create:"
)

const (
	eventTypeSessionQueued                         = eventlog.EventType("session.queued")
	eventTypeSessionEnvironmentProvisioningStarted = eventlog.EventType("session.environment_provisioning_started")
	eventTypeSessionEnvironmentProvisioned         = eventlog.EventType("session.environment_provisioned")
	eventTypeSessionRuntimeStarted                 = eventlog.EventType("session.runtime_started")
	eventTypeSessionReady                          = eventlog.EventType("session.ready")
	eventTypeSessionFailed                         = eventlog.EventType("session.failed")
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

func queuedBackendID(payload queuedPayload) conf.BackendID {
	return conf.BackendID(payload.BackendID)
}
