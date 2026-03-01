package sessionids

import (
	"strings"
	"testing"
)

func TestNewWithPrefix(t *testing.T) {
	t.Parallel()

	id, err := NewWithPrefix("fix-crash", &GeneratorConfig{
		MaxAttempts: 10,
		IsValid: func(id string) error {
			if !strings.HasPrefix(id, "fix-crash-") {
				t.Fatalf("id %q did not have expected prefix", id)
			}
			return nil
		},
	})
	if err != nil {
		t.Fatalf("NewWithPrefix returned error: %v", err)
	}
	if !strings.HasPrefix(id, "fix-crash-") {
		t.Fatalf("id %q did not have expected prefix", id)
	}
}
