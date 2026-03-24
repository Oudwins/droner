package remote

import (
	"context"
)

// provider interface abstracts provider-specific implementations
type provider interface {
	subscribe(key subscriptionKey)
	unsubscribe(key subscriptionKey)
	close()
	ensureAuth(ctx context.Context, remoteURL string) error
}
