package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
)

func withTempDataDir(t *testing.T) string {
	t.Helper()
	dataDir := t.TempDir()
	config := conf.GetConfig()
	original := config.Server.DataDir
	config.Server.DataDir = dataDir
	t.Cleanup(func() { config.Server.DataDir = original })
	return dataDir
}

func TestReadGitHubAuthMissingFile(t *testing.T) {
	withTempDataDir(t)

	auth, ok, err := ReadGitHubAuth()
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false")
	}
	if auth != nil {
		t.Fatalf("expected nil auth")
	}
}

func TestReadGitHubAuthMalformedJSON(t *testing.T) {
	withTempDataDir(t)

	path, err := authFilePath()
	if err != nil {
		t.Fatalf("authFilePath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte("{invalid"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	_, _, err = ReadGitHubAuth()
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestReadGitHubAuthMissingToken(t *testing.T) {
	withTempDataDir(t)

	path, err := authFilePath()
	if err != nil {
		t.Fatalf("authFilePath: %v", err)
	}
	payload := map[string]any{"github": map[string]any{"access_token": ""}}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	auth, ok, err := ReadGitHubAuth()
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if ok {
		t.Fatalf("expected ok=false")
	}
	if auth != nil {
		t.Fatalf("expected nil auth")
	}
}

func TestWriteGitHubAuthRoundTrip(t *testing.T) {
	withTempDataDir(t)

	want := GitHubAuth{
		AccessToken: "token",
		TokenType:   "bearer",
		Scope:       "repo",
		UpdatedAt:   time.Now().UTC().Truncate(time.Second),
	}
	if err := WriteGitHubAuth(want); err != nil {
		t.Fatalf("WriteGitHubAuth: %v", err)
	}

	got, ok, err := ReadGitHubAuth()
	if err != nil {
		t.Fatalf("ReadGitHubAuth: %v", err)
	}
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if got == nil {
		t.Fatalf("expected auth")
	}
	if got.AccessToken != want.AccessToken || got.TokenType != want.TokenType || got.Scope != want.Scope {
		t.Fatalf("unexpected auth: %+v", got)
	}
}

func TestAuthFilePathExpandsHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	config := conf.GetConfig()
	original := config.Server.DataDir
	config.Server.DataDir = "~/droner-test"
	t.Cleanup(func() { config.Server.DataDir = original })

	path, err := authFilePath()
	if err != nil {
		t.Fatalf("authFilePath: %v", err)
	}

	expected := filepath.Join(tmp, "droner-test", "auth.json")
	if path != expected {
		t.Fatalf("expected %q, got %q", expected, path)
	}
}
