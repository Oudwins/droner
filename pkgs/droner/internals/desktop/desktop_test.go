package desktop

import (
	"os/exec"
	"testing"
)

func TestOpenURLEmpty(t *testing.T) {
	if err := OpenURL(""); err == nil {
		t.Fatalf("expected error for empty url")
	}
}

func TestOpenURLUnsupportedPlatform(t *testing.T) {
	originalGOOS := RuntimeGOOS
	t.Cleanup(func() { RuntimeGOOS = originalGOOS })
	RuntimeGOOS = "plan9"

	if err := OpenURL("https://example.com"); err == nil {
		t.Fatalf("expected error for unsupported platform")
	}
}

func TestOpenURLUsesExecSeam(t *testing.T) {
	originalExec := ExecCommand
	originalGOOS := RuntimeGOOS
	t.Cleanup(func() {
		ExecCommand = originalExec
		RuntimeGOOS = originalGOOS
	})

	RuntimeGOOS = "linux"
	var gotName string
	var gotArgs []string
	ExecCommand = func(name string, args ...string) *exec.Cmd {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return exec.Command("sh", "-c", "true")
	}

	if err := OpenURL("https://example.com"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotName != "xdg-open" {
		t.Fatalf("expected xdg-open, got %s", gotName)
	}
	if len(gotArgs) != 1 || gotArgs[0] != "https://example.com" {
		t.Fatalf("expected url arg, got %v", gotArgs)
	}
}
