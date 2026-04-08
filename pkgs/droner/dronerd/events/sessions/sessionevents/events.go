package sessionevents

import (
	"encoding/json"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/internals/remote"
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

const (
	eventTypeSessionQueued                         = eventlog.EventType("session.queued")
	eventTypeSessionEnrichmentRequested            = eventlog.EventType("session.enrichment.requested")
	eventTypeSessionEnrichmentSucceeded            = eventlog.EventType("session.enrichment.succeeded")
	eventTypeSessionEnrichmentFailed               = eventlog.EventType("session.enrichment.failed")
	eventTypeSessionHydrationRequested             = eventlog.EventType("session.hydration.requested")
	eventTypeSessionEnvironmentProvisioningStarted = eventlog.EventType("session.environment_provisioning.started")
	eventTypeSessionEnvironmentProvisioningSuccess = eventlog.EventType("session.environment_provisioning.success")
	eventTypeSessionEnvironmentProvisioningFailed  = eventlog.EventType("session.environment_provisioning.failed")
	eventTypeSessionReady                          = eventlog.EventType("session.ready")
	eventTypeSessionAgentBusy                      = eventlog.EventType("session.agent.busy")
	eventTypeSessionAgentIdle                      = eventlog.EventType("session.agent.idle")
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

type remoteObservationPayload struct {
	Branch     string    `json:"branch"`
	RemoteURL  string    `json:"remoteUrl"`
	PRNumber   *int      `json:"prNumber,omitempty"`
	PRState    string    `json:"prState,omitempty"`
	ObservedAt time.Time `json:"observedAt"`
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

func requestStepPayload(branch string) branchPayload {
	return newBranchPayload(branch)
}

func provisioningStepPayload(branch string, mode string) provisioningPayload {
	return provisioningPayload{Branch: branch, Mode: mode}
}

func queuedBackendID(payload queuedPayload) conf.BackendID {
	return conf.BackendID(payload.BackendID)
}

func newRemoteObservationPayload(branch string, event remote.BranchEvent) remoteObservationPayload {
	payload := remoteObservationPayload{
		Branch:     branch,
		RemoteURL:  event.RemoteURL,
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
