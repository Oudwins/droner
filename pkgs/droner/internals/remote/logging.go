package remote

import "log/slog"

func remoteLogger() *slog.Logger {
	return slog.Default().With("component", "remote")
}

func githubProviderLogger() *slog.Logger {
	return slog.Default().With("component", "github_provider")
}
