package term

import "testing"

func clearEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"TERM",
		"WT_SESSION",
		"VTE_VERSION",
		"KONSOLE_VERSION",
		"KITTY_WINDOW_ID",
		"WEZTERM_EXECUTABLE",
		"DOMTERM",
		"TERM_PROGRAM",
	}
	for _, key := range keys {
		t.Setenv(key, "")
	}
}

func TestSupportsHyperlinks(t *testing.T) {
	clearEnv(t)
	t.Setenv("TERM", "dumb")
	if SupportsHyperlinks() {
		t.Fatalf("expected hyperlinks unsupported for dumb term")
	}

	clearEnv(t)
	t.Setenv("TERM", "alacritty")
	t.Setenv("TERM_PROGRAM", "iTerm")
	if SupportsHyperlinks() {
		t.Fatalf("expected hyperlinks unsupported for alacritty")
	}

	clearEnv(t)
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("TERM_PROGRAM", "iTerm")
	if !SupportsHyperlinks() {
		t.Fatalf("expected hyperlinks supported")
	}
}

func TestClickableLink(t *testing.T) {
	clearEnv(t)
	t.Setenv("TERM", "dumb")
	if got := ClickableLink("label", "https://example.com"); got != "label" {
		t.Fatalf("expected label, got %q", got)
	}

	clearEnv(t)
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("TERM_PROGRAM", "iTerm")
	got := ClickableLink("label", "https://example.com")
	if got == "label" {
		t.Fatalf("expected clickable link escape sequence")
	}
}
