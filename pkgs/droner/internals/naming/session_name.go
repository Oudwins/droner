package naming

import "strings"

// SanitizeSessionNamePrefix turns arbitrary text into a branch-safe prefix.
// Output uses only [a-z0-9-] and never returns a leading/trailing '-'.
// Non-ASCII characters are treated as separators.
func SanitizeSessionNamePrefix(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(s))
	prevDash := false
	for _, r := range s {
		if r >= 'A' && r <= 'Z' {
			r = r - 'A' + 'a'
		}
		isAZ := r >= 'a' && r <= 'z'
		is09 := r >= '0' && r <= '9'
		if isAZ || is09 {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if prevDash {
			continue
		}
		b.WriteByte('-')
		prevDash = true
	}

	out := b.String()
	out = strings.Trim(out, "-")
	return out
}
