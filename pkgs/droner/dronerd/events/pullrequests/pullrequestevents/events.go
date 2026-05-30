package pullrequestevents

import (
	"encoding/json"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/internals/remote"
	"github.com/Oudwins/droner/pkgs/droner/internals/eventlog"
)

type prObservedPayload struct {
	Provider   string                      `json:"provider"`
	StreamID   string                      `json:"streamId"`
	RemoteURL  string                      `json:"remoteUrl"`
	RepoOwner  string                      `json:"repoOwner"`
	RepoName   string                      `json:"repoName"`
	Number     int                         `json:"number"`
	ObservedAt time.Time                   `json:"observedAt"`
	Kind       string                      `json:"kind"`
	Snapshot   *remote.PullRequestSnapshot `json:"snapshot,omitempty"`
	Changes    []PRFieldChange             `json:"changes,omitempty"`
}

type PRFieldChange struct {
	Field string `json:"field"`
	Old   any    `json:"old,omitempty"`
	New   any    `json:"new,omitempty"`
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

func marshalPendingEvent(streamID string, eventType eventlog.EventType, payload any, causationID, correlationID string) (eventlog.PendingEvent, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return eventlog.PendingEvent{}, err
	}
	return eventlog.PendingEvent{StreamID: eventlog.StreamID(streamID), Type: eventType, SchemaVersion: 1, Payload: payloadBytes, CausationID: eventlog.EventID(causationID), CorrelationID: correlationID}, nil
}
