package sdk

import (
	"context"
	"math"
	"net/http"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/timeouts"
)

const (
	DefaultPingTimeout = timeouts.Probe
	startInitialDelay  = 2 * time.Second
	startAttempts      = 6
)

type InfoLogger interface {
	Info(msg string, args ...any)
}

func IsRunning(baseURL string) bool {
	return IsRunningWithTimeout(baseURL, DefaultPingTimeout)
}

func IsRunningWithTimeout(baseURL string, timeout time.Duration) bool {
	if baseURL == "" {
		return false
	}
	if timeout <= 0 {
		timeout = DefaultPingTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	client := NewClient(
		WithBaseURL(baseURL),
		WithHTTPClient(&http.Client{Timeout: timeout}),
	)
	_, err := client.Version(ctx)
	return err == nil
}

func WaitForStart(baseURL string, logger InfoLogger) bool {
	time.Sleep(startInitialDelay)
	for i := range startAttempts {
		if logger != nil {
			logger.Info("Waiting for server to start", "attempt", i)
		}
		if IsRunning(baseURL) {
			return true
		}
		time.Sleep(time.Duration(math.Pow(2, float64(i))) * time.Second)
	}

	return false
}
