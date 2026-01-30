package logbuf

import (
	"log/slog"
	"sync"
	"testing"
	"time"
)

func TestWithPreservesAttrsAndBuffer(t *testing.T) {
	logger := New(slog.String("parent", "yes"))
	child := logger.With(slog.String("child", "yes"))
	_ = child.Info("hello")

	payload := child.Flush()
	attrs := attrsToMap(payload.Value.Group())

	if attrs["parent"] != "yes" {
		t.Fatalf("expected parent attr")
	}
	if attrs["child"] != "yes" {
		t.Fatalf("expected child attr")
	}

	entries, ok := attrs["entries"].([]map[string]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("expected one entry, got %v", attrs["entries"])
	}
}

func TestAddAppendsAttrs(t *testing.T) {
	logger := New()
	child := logger.With(slog.String("a", "1"))
	child.Add(slog.String("b", "2"))

	payload := child.Flush()
	attrs := attrsToMap(payload.Value.Group())

	if attrs["a"] != "1" || attrs["b"] != "2" {
		t.Fatalf("expected attrs to include a and b, got %v", attrs)
	}
}

func TestFlushResetsBuffer(t *testing.T) {
	logger := New()
	child := logger.With(slog.String("k", "v"))
	_ = child.Info("first")

	buf := child.bufferOrAncestor()
	if buf == nil {
		t.Fatalf("expected buffer")
	}
	if buf.seq == 0 {
		t.Fatalf("expected seq to increment")
	}

	_ = child.Flush()
	if buf.seq != 0 {
		t.Fatalf("expected seq reset, got %d", buf.seq)
	}
	if len(buf.entries) != 0 {
		t.Fatalf("expected entries cleared")
	}

	_ = child.Info("second")
	if buf.seq != 1 {
		t.Fatalf("expected seq to restart at 1, got %d", buf.seq)
	}
}

func TestEntriesToPayloadDoesNotOverwriteReserved(t *testing.T) {
	now := time.Now().UTC()
	entries := []Entry{
		{
			Level:   "info",
			Message: "hello",
			At:      now,
			Seq:     1,
			Attrs: []slog.Attr{
				slog.String("message", "override"),
				slog.String("extra", "ok"),
			},
		},
	}

	payload := entriesToPayload(entries)
	if len(payload) != 1 {
		t.Fatalf("expected one payload entry")
	}
	item := payload[0]
	if item["message"] != "hello" {
		t.Fatalf("expected reserved message to stay, got %v", item["message"])
	}
	if item["extra"] != "ok" {
		t.Fatalf("expected extra attr, got %v", item["extra"])
	}
}

func TestConcurrentLogging(t *testing.T) {
	logger := New()
	child := logger.With(slog.String("k", "v"))

	const count = 50
	var wg sync.WaitGroup
	wg.Add(count)
	for i := 0; i < count; i++ {
		go func(i int) {
			defer wg.Done()
			_ = child.Info("msg", slog.Int("i", i))
		}(i)
	}
	wg.Wait()

	payload := child.Flush()
	attrs := attrsToMap(payload.Value.Group())
	entries, ok := attrs["entries"].([]map[string]any)
	if !ok {
		t.Fatalf("expected entries slice")
	}
	if len(entries) != count {
		t.Fatalf("expected %d entries, got %d", count, len(entries))
	}
}
