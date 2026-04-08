package sessionevents

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/events/sessions/agentevents"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/eventlog"
)

func TestSessionStateAppliesAgentBusyIdleWithoutChangingLifecycle(t *testing.T) {
	queuedPayload, err := json.Marshal(newQueuedPayload(CreateSessionInput{
		StreamID:        "stream-1",
		Harness:         conf.HarnessOpenCode,
		RequestedBranch: "agent-branch",
		BackendID:       conf.BackendLocal,
		RepoPath:        "/tmp/repo",
	}))
	if err != nil {
		t.Fatalf("Marshal queued payload: %v", err)
	}
	enrichmentPayload, err := json.Marshal(newEnrichmentSucceededPayload("agent-branch", "/tmp/repo..agent-branch"))
	if err != nil {
		t.Fatalf("Marshal enrichment payload: %v", err)
	}
	requestPayload, err := json.Marshal(requestStepPayload("agent-branch"))
	if err != nil {
		t.Fatalf("Marshal request payload: %v", err)
	}

	state := sessionState{}
	apply := func(eventType eventlog.EventType, payload []byte, occurredAt time.Time) bool {
		t.Helper()
		changed, err := state.Apply(eventlog.Envelope{Type: eventType, OccurredAt: occurredAt, Payload: payload})
		if err != nil {
			t.Fatalf("Apply(%s): %v", eventType, err)
		}
		return changed
	}

	now := time.Now().UTC()
	apply(eventTypeSessionQueued, queuedPayload, now)
	apply(eventTypeSessionEnrichmentSucceeded, enrichmentPayload, now.Add(500*time.Millisecond))
	apply(eventTypeSessionReady, requestPayload, now.Add(time.Second))

	if !apply(eventTypeSessionAgentBusy, requestPayload, now.Add(2*time.Second)) {
		t.Fatal("expected busy event to change state")
	}
	if state.LifecycleState != LifecycleStateReady {
		t.Fatalf("lifecycle state = %s, want %s", state.LifecycleState, LifecycleStateReady)
	}
	if state.PublicState != PublicStateActiveBusy {
		t.Fatalf("public state = %s, want %s", state.PublicState, PublicStateActiveBusy)
	}

	if !apply(eventTypeSessionAgentIdle, requestPayload, now.Add(3*time.Second)) {
		t.Fatal("expected idle event to change state")
	}
	if state.PublicState != PublicStateActiveIdle {
		t.Fatalf("public state = %s, want %s", state.PublicState, PublicStateActiveIdle)
	}

	apply(eventTypeSessionCompletionStarted, requestPayload, now.Add(4*time.Second))
	if apply(eventTypeSessionAgentBusy, requestPayload, now.Add(5*time.Second)) {
		t.Fatal("expected busy event to be ignored after completion starts")
	}
	if state.PublicState != PublicStateCompleting {
		t.Fatalf("public state = %s, want %s", state.PublicState, PublicStateCompleting)
	}
	if state.LifecycleState != LifecycleStateCompletionStarted {
		t.Fatalf("lifecycle state = %s, want %s", state.LifecycleState, LifecycleStateCompletionStarted)
	}
}

func TestHandleAgentEventResolvesSessionByWorktreePath(t *testing.T) {
	system, _, dataDir, _ := newRemoteTestSystem(t)

	const (
		streamID = "stream-agent-1"
		branch   = "agent-events-branch"
	)

	if _, err := system.CreateSession(context.Background(), CreateSessionInput{
		StreamID:        streamID,
		Harness:         conf.HarnessOpenCode,
		RequestedBranch: branch,
		BackendID:       conf.BackendLocal,
		RepoPath:        "/tmp/repo",
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	ref := waitForPublicState(t, system, branch, PublicStateActiveIdle)
	if err := system.handleAgentEvent(context.Background(), agentevents.Event{WorktreePath: ref.WorktreePath, State: agentevents.StateBusy}); err != nil {
		t.Fatalf("handleAgentEvent busy: %v", err)
	}
	waitForPublicState(t, system, branch, PublicStateActiveBusy)

	if err := system.handleAgentEvent(context.Background(), agentevents.Event{WorktreePath: ref.WorktreePath, State: agentevents.StateIdle}); err != nil {
		t.Fatalf("handleAgentEvent idle: %v", err)
	}
	waitForPublicState(t, system, branch, PublicStateActiveIdle)

	assertEventOrder(t, loadEventTypes(t, dataDir, streamID), eventTypeSessionReady, eventTypeSessionAgentBusy, eventTypeSessionAgentIdle)
}
