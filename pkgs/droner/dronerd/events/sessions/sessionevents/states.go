package sessionevents

import "github.com/Oudwins/droner/pkgs/droner/dronerd/events/eventtypes"

type PublicState string

const (
	PublicStateQueued     PublicState = "queued"
	PublicStateActiveIdle PublicState = "active.idle"
	PublicStateActiveBusy PublicState = "active.busy"
	PublicStateCompleting PublicState = "completing"
	PublicStateCompleted  PublicState = "completed"
	PublicStateFailed     PublicState = "failed"
	PublicStateDeleting   PublicState = "deleting"
	PublicStateDeleted    PublicState = "deleted"
)

func (s PublicState) String() string {
	return string(s)
}

func (s PublicState) IsActive() bool {
	switch s {
	case PublicStateActiveIdle, PublicStateActiveBusy:
		return true
	default:
		return false
	}
}

func (s PublicState) IsTerminal() bool {
	switch s {
	case PublicStateCompleted, PublicStateFailed, PublicStateDeleted:
		return true
	default:
		return false
	}
}

type LifecycleState string

const (
	LifecycleStateQueued                         LifecycleState = LifecycleState(eventtypes.SessionQueued)
	LifecycleStateEnrichmentRequested            LifecycleState = LifecycleState(eventtypes.SessionEnrichmentRequested)
	LifecycleStateEnrichmentSucceeded            LifecycleState = LifecycleState(eventtypes.SessionEnrichmentSucceeded)
	LifecycleStateEnrichmentFailed               LifecycleState = LifecycleState(eventtypes.SessionEnrichmentFailed)
	LifecycleStateHydrationRequested             LifecycleState = LifecycleState(eventtypes.SessionHydrationRequested)
	LifecycleStateEnvironmentProvisioningStarted LifecycleState = LifecycleState(eventtypes.SessionEnvironmentProvisioningStarted)
	LifecycleStateEnvironmentProvisioningSuccess LifecycleState = LifecycleState(eventtypes.SessionEnvironmentProvisioningSuccess)
	LifecycleStateEnvironmentProvisioningFailed  LifecycleState = LifecycleState(eventtypes.SessionEnvironmentProvisioningFailed)
	LifecycleStateReady                          LifecycleState = LifecycleState(eventtypes.SessionReady)
	LifecycleStateCompletionRequested            LifecycleState = LifecycleState(eventtypes.SessionCompletionRequested)
	LifecycleStateCompletionStarted              LifecycleState = LifecycleState(eventtypes.SessionCompletionStarted)
	LifecycleStateCompletionSuccess              LifecycleState = LifecycleState(eventtypes.SessionCompletionSuccess)
	LifecycleStateCompletionFailed               LifecycleState = LifecycleState(eventtypes.SessionCompletionFailed)
	LifecycleStateDeletionRequested              LifecycleState = LifecycleState(eventtypes.SessionDeletionRequested)
	LifecycleStateDeletionStarted                LifecycleState = LifecycleState(eventtypes.SessionDeletionStarted)
	LifecycleStateDeletionSuccess                LifecycleState = LifecycleState(eventtypes.SessionDeletionSuccess)
	LifecycleStateDeletionFailed                 LifecycleState = LifecycleState(eventtypes.SessionDeletionFailed)
)

func (s LifecycleState) String() string {
	return string(s)
}

func (s LifecycleState) AllowsAgentRuntime() bool {
	return s == LifecycleStateReady
}

func (s LifecycleState) IsTerminal() bool {
	switch s {
	case LifecycleStateCompletionSuccess, LifecycleStateEnvironmentProvisioningFailed, LifecycleStateCompletionFailed, LifecycleStateDeletionSuccess, LifecycleStateDeletionFailed:
		return true
	default:
		return false
	}
}
