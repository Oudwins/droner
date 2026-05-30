package sessionevents

import (
	"encoding/json"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/eventlog"
)

const (
	taskIDPrefixCreate   = "session-create:"
	taskIDPrefixComplete = "session-complete:"
	taskIDPrefixDelete   = "session-delete:"
	taskIDPrefixReset    = "session-reset:"
)

const (
	provisioningModeInitial = "initial"
	provisioningModeRestart = "restart"
)

type queuedPayload struct {
	StreamID        string `json:"streamId"`
	Harness         string `json:"harness"`
	RequestedBranch string `json:"requestedBranch,omitempty"`
	BackendID       string `json:"backendId"`
	RepoPath        string `json:"repoPath"`
	RemoteURL       string `json:"remoteUrl,omitempty"`
	AgentConfigJSON string `json:"agentConfigJson,omitempty"`
}

type failedPayload struct {
	Error          string `json:"error"`
	BackendDetails string `json:"backendDetails,omitempty"`
}

type branchPayload struct {
	Branch string `json:"branch"`
}

type enrichmentSucceededPayload struct {
	Branch       string `json:"branch"`
	WorktreePath string `json:"worktreePath"`
}

type provisioningPayload struct {
	Branch string `json:"branch"`
	Mode   string `json:"mode,omitempty"`
}

type sessionPRLinkedPayload struct {
	PRStreamID string    `json:"prStreamId"`
	PRNumber   int       `json:"prNumber"`
	State      string    `json:"state,omitempty"`
	CIState    string    `json:"ciState,omitempty"`
	LinkedAt   time.Time `json:"linkedAt"`
}

type sessionPRStateChangedPayload struct {
	PRStreamID string    `json:"prStreamId"`
	PRNumber   int       `json:"prNumber"`
	State      string    `json:"state"`
	ChangedAt  time.Time `json:"changedAt"`
}

type sessionPRCIStateChangedPayload struct {
	PRStreamID string    `json:"prStreamId"`
	PRNumber   int       `json:"prNumber"`
	CIState    string    `json:"ciState"`
	ChangedAt  time.Time `json:"changedAt"`
}

func newQueuedPayload(input CreateSessionInput) queuedPayload {
	return queuedPayload{
		StreamID:        input.StreamID,
		Harness:         input.Harness.String(),
		RequestedBranch: input.RequestedBranch,
		BackendID:       input.BackendID.String(),
		RepoPath:        input.RepoPath,
		RemoteURL:       input.RemoteURL,
		AgentConfigJSON: input.AgentConfigJSON,
	}
}

func newFailedPayload(err error) failedPayload {
	message := err.Error()
	return failedPayload{Error: message, BackendDetails: message}
}

func newBranchPayload(branch string) branchPayload {
	return branchPayload{Branch: branch}
}

func newEnrichmentSucceededPayload(branch string, worktreePath string) enrichmentSucceededPayload {
	return enrichmentSucceededPayload{Branch: branch, WorktreePath: worktreePath}
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

func decodeBranchPayload(evt eventlog.Envelope) (branchPayload, error) {
	var payload branchPayload
	err := json.Unmarshal(evt.Payload, &payload)
	return payload, err
}

func decodeEnrichmentSucceededPayload(evt eventlog.Envelope) (enrichmentSucceededPayload, error) {
	var payload enrichmentSucceededPayload
	err := json.Unmarshal(evt.Payload, &payload)
	return payload, err
}

func decodeProvisioningPayload(evt eventlog.Envelope) (provisioningPayload, error) {
	var payload provisioningPayload
	err := json.Unmarshal(evt.Payload, &payload)
	return payload, err
}

func decodeSessionPRLinkedPayload(evt eventlog.Envelope) (sessionPRLinkedPayload, error) {
	var payload sessionPRLinkedPayload
	err := json.Unmarshal(evt.Payload, &payload)
	return payload, err
}

func decodeSessionPRStateChangedPayload(evt eventlog.Envelope) (sessionPRStateChangedPayload, error) {
	var payload sessionPRStateChangedPayload
	err := json.Unmarshal(evt.Payload, &payload)
	return payload, err
}

func decodeSessionPRCIStateChangedPayload(evt eventlog.Envelope) (sessionPRCIStateChangedPayload, error) {
	var payload sessionPRCIStateChangedPayload
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

func requestStepPayload(branch string) branchPayload {
	return newBranchPayload(branch)
}

func provisioningStepPayload(branch string, mode string) provisioningPayload {
	return provisioningPayload{Branch: branch, Mode: mode}
}

func queuedBackendID(payload queuedPayload) conf.BackendID {
	return conf.BackendID(payload.BackendID)
}
