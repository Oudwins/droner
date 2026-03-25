package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
)

func TestNewMissingFileStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")

	store, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if got := store.Path(); got != path {
		t.Fatalf("Path = %q, want %q", got, path)
	}
	if auth, ok := store.GitHub(); ok || auth != nil {
		t.Fatalf("expected empty github auth, got ok=%v auth=%+v", ok, auth)
	}
}

func TestNewBlankFileStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte(" \n\t "), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	store, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	if auth, ok := store.GitHub(); ok || auth != nil {
		t.Fatalf("expected empty github auth, got ok=%v auth=%+v", ok, auth)
	}
}

func TestNewMalformedJSONReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(path, []byte("{invalid"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := New(path)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "failed to parse auth file JSON") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewRejectsInvalidGitHubAuth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	data := []byte(`{"github":{"access_token":"   "}}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, err := New(path)
	if err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(err.Error(), "failed to parse auth file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSetGitHubRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	store, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	want := GitHubAuth{
		AccessToken: "token",
		TokenType:   "bearer",
		Scope:       "repo",
		UpdatedAt:   time.Now().UTC().Truncate(time.Second),
	}
	if err := store.SetGitHub(want); err != nil {
		t.Fatalf("SetGitHub: %v", err)
	}

	loaded, err := New(path)
	if err != nil {
		t.Fatalf("New reload: %v", err)
	}

	got, ok := loaded.GitHub()
	if !ok || got == nil {
		t.Fatalf("expected github auth")
	}
	assertGitHubAuth(t, got, want)
}

func TestSetGitHubUpdatesInMemoryState(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "auth.json"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	want := GitHubAuth{AccessToken: "token", UpdatedAt: time.Now().UTC().Truncate(time.Second)}
	if err := store.SetGitHub(want); err != nil {
		t.Fatalf("SetGitHub: %v", err)
	}

	got, ok := store.GitHub()
	if !ok || got == nil {
		t.Fatalf("expected github auth")
	}
	assertGitHubAuth(t, got, want)
}

func TestSetGitHubFailedSaveKeepsInMemoryState(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "auth.json"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	original := GitHubAuth{AccessToken: "first", UpdatedAt: time.Now().UTC().Truncate(time.Second)}
	if err := store.SetGitHub(original); err != nil {
		t.Fatalf("SetGitHub original: %v", err)
	}

	store.path = t.TempDir()
	err = store.SetGitHub(GitHubAuth{AccessToken: "second", UpdatedAt: time.Now().UTC().Truncate(time.Second)})
	if err == nil {
		t.Fatalf("expected error")
	}

	got, ok := store.GitHub()
	if !ok || got == nil {
		t.Fatalf("expected github auth")
	}
	assertGitHubAuth(t, got, original)
}

func TestNewExpandsHomeInExplicitPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	store, err := New("~/nested/auth.json")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	want := filepath.Join(home, "nested", "auth.json")
	if got := store.Path(); got != want {
		t.Fatalf("Path = %q, want %q", got, want)
	}
}

func TestDefaultUsesConfigDataDir(t *testing.T) {
	config := conf.GetConfig()
	original := config.Server.DataDir
	config.Server.DataDir = t.TempDir()
	t.Cleanup(func() {
		config.Server.DataDir = original
	})

	store, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}

	want := filepath.Join(config.Server.DataDir, "auth.json")
	if got := store.Path(); got != want {
		t.Fatalf("Path = %q, want %q", got, want)
	}
}

func TestReloadPicksUpExternalFileChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	store, err := New(path)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	first := GitHubAuth{AccessToken: "first", UpdatedAt: time.Now().UTC().Truncate(time.Second)}
	if err := store.SetGitHub(first); err != nil {
		t.Fatalf("SetGitHub first: %v", err)
	}

	replacement := File{GitHub: &GitHubAuth{AccessToken: "second", TokenType: "bearer", UpdatedAt: time.Now().UTC().Add(time.Minute).Truncate(time.Second)}}
	data, err := json.MarshalIndent(replacement, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	if err := store.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	got, ok := store.GitHub()
	if !ok || got == nil {
		t.Fatalf("expected github auth")
	}
	assertGitHubAuth(t, got, *replacement.GitHub)
}

func assertGitHubAuth(t *testing.T, got *GitHubAuth, want GitHubAuth) {
	t.Helper()
	if got.AccessToken != want.AccessToken {
		t.Fatalf("AccessToken = %q, want %q", got.AccessToken, want.AccessToken)
	}
	if got.TokenType != want.TokenType {
		t.Fatalf("TokenType = %q, want %q", got.TokenType, want.TokenType)
	}
	if got.Scope != want.Scope {
		t.Fatalf("Scope = %q, want %q", got.Scope, want.Scope)
	}
	if !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Fatalf("UpdatedAt = %s, want %s", got.UpdatedAt, want.UpdatedAt)
	}
}
