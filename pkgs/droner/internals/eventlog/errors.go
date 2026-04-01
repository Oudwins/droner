package eventlog

import "errors"

var (
	ErrBackendRequired      = errors.New("eventlog backend is required")
	ErrTopicRequired        = errors.New("eventlog topic is required")
	ErrStreamIDRequired     = errors.New("eventlog stream id is required")
	ErrEventTypeRequired    = errors.New("eventlog event type is required")
	ErrSubscriptionIDNeeded = errors.New("eventlog subscription id is required")
	ErrHandlerRequired      = errors.New("eventlog subscription handler is required")
)
