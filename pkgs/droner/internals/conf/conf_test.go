package conf

import (
	"testing"
)

func TestConfigDefaults(t *testing.T) {
	original := config
	config = nil
	t.Cleanup(func() { config = original })

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	got := GetConfig()
	if got.Worktrees.Dir != "~/.local/share/droner/worktrees" {
		t.Fatalf("expected default worktrees dir, got %q", got.Worktrees.Dir)
	}
	if got.Server.DataDir != "~/.local/share/droner" {
		t.Fatalf("expected default data dir, got %q", got.Server.DataDir)
	}
	if got.Agent.DefaultModel == "" {
		t.Fatalf("expected default model to be set")
	}
	if got.Version == "" {
		t.Fatalf("expected version to be set")
	}
}
