package naming

import "testing"

func TestSanitizeSessionNamePrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"   ", ""},
		{"Hello World", "hello-world"},
		{"Fix: crash on /sessions", "fix-crash-on-sessions"},
		{"already-kebab", "already-kebab"},
		{"multi   space", "multi-space"},
		{"punctuation!!!", "punctuation"},
		{"line1\nline2", "line1"},
		{"---Leading and trailing---", "leading-and-trailing"},
		{"non-ascii: cafe\u0301", "non-ascii-cafe"},
	}

	for _, tt := range tests {
		if got := SanitizeSessionNamePrefix(tt.in); got != tt.want {
			t.Fatalf("SanitizeSessionNamePrefix(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
