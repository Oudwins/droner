package server

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
)

func TestHelperExpandPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	value, err := expandPath("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if value != "" {
		t.Fatalf("expected empty, got %q", value)
	}

	value, err = expandPath("~")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if value != home {
		t.Fatalf("expected %q, got %q", home, value)
	}

	value, err = expandPath("~/repo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expected := filepath.Join(home, "repo")
	if value != expected {
		t.Fatalf("expected %q, got %q", expected, value)
	}
}

func TestHelperGenerateSessionID(t *testing.T) {
	root := t.TempDir()
	id, err := generateSessionID("repo", root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id == "" {
		t.Fatalf("expected non-empty id")
	}
	if _, err := os.Stat(filepath.Join(root, "repo#"+id)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected path to not exist")
	}
}

func TestHelperSessionIDFromName(t *testing.T) {
	if got := sessionIDFromName("repo#abc"); got != "abc" {
		t.Fatalf("expected abc, got %q", got)
	}
	if got := sessionIDFromName("repo"); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestHelperFindWorktreeBySessionID(t *testing.T) {
	root := t.TempDir()
	if _, err := findWorktreeBySessionID(root, "abc"); err == nil {
		t.Fatalf("expected error for missing session")
	}

	first := filepath.Join(root, "repo#abc")
	if err := os.MkdirAll(first, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path, err := findWorktreeBySessionID(root, "abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != first {
		t.Fatalf("expected %q, got %q", first, path)
	}

	second := filepath.Join(root, "other#abc")
	if err := os.MkdirAll(second, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if _, err := findWorktreeBySessionID(root, "abc"); err == nil {
		t.Fatalf("expected error for multiple matches")
	}
}

func TestHelperResolveDeleteWorktreePath(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "repo#abc")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, err := resolveDeleteWorktreePath(root, schemas.SessionDeleteRequest{Path: path})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != path {
		t.Fatalf("expected %q, got %q", path, got)
	}

	got, err = resolveDeleteWorktreePath(root, schemas.SessionDeleteRequest{SessionID: "abc"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != path {
		t.Fatalf("expected %q, got %q", path, got)
	}

	_, err = resolveDeleteWorktreePath(root, schemas.SessionDeleteRequest{SessionID: "missing"})
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected os.ErrNotExist, got %v", err)
	}
}

func TestHelperJsonUnmarshalAndNullIfEmpty(t *testing.T) {
	var payload map[string]any
	if err := jsonUnmarshal("", &payload); err == nil {
		t.Fatalf("expected error on empty json")
	}

	if err := jsonUnmarshal("{}", &payload); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if value := nullIfEmpty(""); value != nil {
		t.Fatalf("expected nil for empty value")
	}
	if value := nullIfEmpty("ok"); value != "ok" {
		t.Fatalf("expected ok, got %v", value)
	}
}
