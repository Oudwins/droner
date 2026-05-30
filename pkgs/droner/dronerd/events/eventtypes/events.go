package eventtypes

import "github.com/Oudwins/droner/pkgs/droner/internals/eventlog"

const (
	SessionQueued                         = eventlog.EventType("session.queued")
	SessionEnrichmentRequested            = eventlog.EventType("session.enrichment.requested")
	SessionEnrichmentSucceeded            = eventlog.EventType("session.enrichment.succeeded")
	SessionEnrichmentFailed               = eventlog.EventType("session.enrichment.failed")
	SessionHydrationRequested             = eventlog.EventType("session.hydration.requested")
	SessionEnvironmentProvisioningStarted = eventlog.EventType("session.environment_provisioning.started")
	SessionEnvironmentProvisioningSuccess = eventlog.EventType("session.environment_provisioning.success")
	SessionEnvironmentProvisioningFailed  = eventlog.EventType("session.environment_provisioning.failed")
	SessionReady                          = eventlog.EventType("session.ready")
	SessionAgentBusy                      = eventlog.EventType("session.agent.busy")
	SessionAgentIdle                      = eventlog.EventType("session.agent.idle")
	SessionCompletionRequested            = eventlog.EventType("session.completion.requested")
	SessionCompletionStarted              = eventlog.EventType("session.completion.started")
	SessionCompletionSuccess              = eventlog.EventType("session.completion.success")
	SessionCompletionFailed               = eventlog.EventType("session.completion.failed")
	SessionDeletionRequested              = eventlog.EventType("session.deletion.requested")
	SessionDeletionStarted                = eventlog.EventType("session.deletion.started")
	SessionDeletionSuccess                = eventlog.EventType("session.deletion.success")
	SessionDeletionFailed                 = eventlog.EventType("session.deletion.failed")
	SessionPRLinked                       = eventlog.EventType("session.pr.linked")
	SessionPRStateChanged                 = eventlog.EventType("session.pr.state_changed")
	SessionPRCIStateChanged               = eventlog.EventType("session.pr.ci_state_changed")
	SessionPRClosed                       = eventlog.EventType("session.pr.closed")
	SessionPRMerged                       = eventlog.EventType("session.pr.merged")
)

const (
	PRObserved = eventlog.EventType("pr.observed")
	PRClosed   = eventlog.EventType("pr.closed")
	PRMerged   = eventlog.EventType("pr.merged")
)
