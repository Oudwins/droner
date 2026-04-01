package sessionevents

import "github.com/Oudwins/droner/pkgs/droner/internals/eventlog"

type nextTrigger struct {
	EventType eventlog.EventType
	Payload   any
}

func (p sessionProjection) NextHydrationTrigger() (nextTrigger, bool) {
	simpleID := p.SimpleID
	switch p.LifecycleState {
	case string(eventTypeSessionQueued):
		return nextTrigger{EventType: eventTypeSessionEnvironmentProvisioningStarted, Payload: provisioningStepPayload(simpleID, provisioningModeInitial)}, true
	case string(eventTypeSessionEnvironmentProvisioningStarted):
		return nextTrigger{EventType: eventTypeSessionEnvironmentProvisioningStarted, Payload: provisioningStepPayload(simpleID, provisioningModeRestart)}, true
	case string(eventTypeSessionReady):
		return nextTrigger{EventType: eventTypeSessionEnvironmentProvisioningStarted, Payload: provisioningStepPayload(simpleID, provisioningModeRestart)}, true
	case string(eventTypeSessionCompletionRequested), string(eventTypeSessionCompletionStarted):
		return nextTrigger{EventType: eventTypeSessionCompletionStarted, Payload: requestStepPayload(simpleID)}, true
	case string(eventTypeSessionDeletionRequested), string(eventTypeSessionDeletionStarted):
		return nextTrigger{EventType: eventTypeSessionDeletionStarted, Payload: requestStepPayload(simpleID)}, true
	default:
		return nextTrigger{}, false
	}
}
