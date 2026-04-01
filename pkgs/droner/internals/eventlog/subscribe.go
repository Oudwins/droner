package eventlog

import (
	"context"
	"strings"
)

const defaultReadLimit = 64

func (l *log) Subscribe(ctx context.Context, sub Subscription) error {
	if strings.TrimSpace(string(sub.ID)) == "" {
		return ErrSubscriptionIDNeeded
	}
	if sub.Handle == nil {
		return ErrHandlerRequired
	}
	if ctx == nil {
		ctx = context.Background()
	}

	afterSequence, err := l.backend.LoadCheckpoint(ctx, l.topic, sub.ID)
	if err != nil {
		return err
	}

	for {
		events, err := l.backend.ReadGlobal(ctx, l.topic, afterSequence, defaultReadLimit)
		if err != nil {
			return err
		}
		if len(events) == 0 {
			continue
		}

		for _, evt := range events {
			if sub.Filter != nil && !sub.Filter(evt) {
				if err := l.backend.StoreCheckpoint(ctx, l.topic, sub.ID, evt.Sequence); err != nil {
					return err
				}
				afterSequence = evt.Sequence
				continue
			}

			if err := sub.Handle(ctx, evt); err != nil {
				return err
			}
			if err := l.backend.StoreCheckpoint(ctx, l.topic, sub.ID, evt.Sequence); err != nil {
				return err
			}
			afterSequence = evt.Sequence
		}
	}
}
