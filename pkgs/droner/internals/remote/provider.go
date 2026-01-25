package remote

import (
	"context"
	"time"
)

// provider interface abstracts provider-specific implementations
type provider interface {
	pollEvents(ctx context.Context, remoteURL string, branchName string) ([]BranchEvent, error)
	pollInterval() time.Duration
}
