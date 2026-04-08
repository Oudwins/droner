package sessionevents

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
	LifecycleStateQueued                         LifecycleState = LifecycleState(eventTypeSessionQueued)
	LifecycleStateEnrichmentRequested            LifecycleState = LifecycleState(eventTypeSessionEnrichmentRequested)
	LifecycleStateEnrichmentSucceeded            LifecycleState = LifecycleState(eventTypeSessionEnrichmentSucceeded)
	LifecycleStateEnrichmentFailed               LifecycleState = LifecycleState(eventTypeSessionEnrichmentFailed)
	LifecycleStateHydrationRequested             LifecycleState = LifecycleState(eventTypeSessionHydrationRequested)
	LifecycleStateEnvironmentProvisioningStarted LifecycleState = LifecycleState(eventTypeSessionEnvironmentProvisioningStarted)
	LifecycleStateEnvironmentProvisioningSuccess LifecycleState = LifecycleState(eventTypeSessionEnvironmentProvisioningSuccess)
	LifecycleStateEnvironmentProvisioningFailed  LifecycleState = LifecycleState(eventTypeSessionEnvironmentProvisioningFailed)
	LifecycleStateReady                          LifecycleState = LifecycleState(eventTypeSessionReady)
	LifecycleStateCompletionRequested            LifecycleState = LifecycleState(eventTypeSessionCompletionRequested)
	LifecycleStateCompletionStarted              LifecycleState = LifecycleState(eventTypeSessionCompletionStarted)
	LifecycleStateCompletionSuccess              LifecycleState = LifecycleState(eventTypeSessionCompletionSuccess)
	LifecycleStateCompletionFailed               LifecycleState = LifecycleState(eventTypeSessionCompletionFailed)
	LifecycleStateDeletionRequested              LifecycleState = LifecycleState(eventTypeSessionDeletionRequested)
	LifecycleStateDeletionStarted                LifecycleState = LifecycleState(eventTypeSessionDeletionStarted)
	LifecycleStateDeletionSuccess                LifecycleState = LifecycleState(eventTypeSessionDeletionSuccess)
	LifecycleStateDeletionFailed                 LifecycleState = LifecycleState(eventTypeSessionDeletionFailed)
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
