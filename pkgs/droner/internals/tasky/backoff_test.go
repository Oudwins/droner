package tasky

import (
	"math"
	"testing"
	"time"
)

func TestBackoffExponential(t *testing.T) {
	backoff := BackoffExponential(BackoffConfig{
		Base:   100 * time.Millisecond,
		Max:    1 * time.Second,
		Factor: 2,
	})

	if got := backoff(1); got != 100*time.Millisecond {
		t.Fatalf("expected 100ms, got %v", got)
	}
	if got := backoff(2); got != 200*time.Millisecond {
		t.Fatalf("expected 200ms, got %v", got)
	}
	if got := backoff(3); got != 400*time.Millisecond {
		t.Fatalf("expected 400ms, got %v", got)
	}
	if got := backoff(5); got != 1*time.Second {
		t.Fatalf("expected max 1s, got %v", got)
	}
}

func TestBackoffExponentialDefaults(t *testing.T) {
	backoff := BackoffExponential(BackoffConfig{
		Base:   50 * time.Millisecond,
		Factor: 0,
	})
	if got := backoff(2); got != 100*time.Millisecond {
		t.Fatalf("expected 100ms with default factor, got %v", got)
	}
	if got := backoff(0); got != 0 {
		t.Fatalf("expected 0 for attempts <= 0, got %v", got)
	}
	if got := backoff(1); got != 50*time.Millisecond {
		t.Fatalf("expected 50ms, got %v", got)
	}
}

func TestBackoffExponentialOverflow(t *testing.T) {
	backoff := BackoffExponential(BackoffConfig{
		Base:   time.Duration(math.MaxInt64),
		Factor: 2,
	})
	if got := backoff(2); got <= 0 {
		t.Fatalf("expected positive duration, got %v", got)
	}
}
