package logbuf

import (
	"log/slog"
	"sync"
	"time"
)

type Entry struct {
	Level   string
	Message string
	At      time.Time
	Seq     uint64
	Attrs   []slog.Attr
}

type Logger struct {
	mu     sync.Mutex
	parent *Logger
	attrs  []slog.Attr
	buffer *buffer
}

type buffer struct {
	mu      sync.Mutex
	entries []Entry
	seq     uint64
}

func New(attrs ...slog.Attr) *Logger {
	logger := &Logger{}
	if len(attrs) > 0 {
		logger.attrs = append(logger.attrs, attrs...)
	}
	return logger
}

func (l *Logger) With(attrs ...slog.Attr) *Logger {
	if len(attrs) == 0 {
		return l
	}
	child := &Logger{parent: l, buffer: l.buffer}
	if child.buffer == nil {
		child.buffer = &buffer{}
	}
	child.attrs = append(child.attrs, attrs...)
	return child
}

func (l *Logger) Add(attrs ...slog.Attr) {
	if len(attrs) == 0 {
		return
	}
	l.mu.Lock()
	l.attrs = append(l.attrs, attrs...)
	l.mu.Unlock()
}

func (l *Logger) Debug(message string, attrs ...slog.Attr) error {
	l.appendEntry("debug", message, attrs...)
	return nil
}

func (l *Logger) Info(message string, attrs ...slog.Attr) error {
	l.appendEntry("info", message, attrs...)
	return nil
}

func (l *Logger) Warn(message string, attrs ...slog.Attr) error {
	l.appendEntry("warn", message, attrs...)
	return nil
}

func (l *Logger) Error(message string, attrs ...slog.Attr) error {
	l.appendEntry("error", message, attrs...)
	return nil
}

func (l *Logger) Warning(message string, attrs ...slog.Attr) error {
	return l.Warn(message, attrs...)
}

func (l *Logger) Err(message string, attrs ...slog.Attr) error {
	return l.Error(message, attrs...)
}

func (l *Logger) Write(p []byte) (int, error) {
	l.appendEntry("info", string(p))
	return len(p), nil
}

func (l *Logger) Flush() slog.Attr {
	buf := l.bufferOrAncestor()
	entries := []Entry{}
	if buf != nil {
		buf.mu.Lock()
		entries = make([]Entry, len(buf.entries))
		copy(entries, buf.entries)
		buf.entries = buf.entries[:0]
		buf.seq = 0
		buf.mu.Unlock()
	}

	payloadAttrs := l.collectAttrs()
	payloadAttrs = append(payloadAttrs, slog.Any("entries", entriesToPayload(entries)))
	payloadArgs := make([]any, 0, len(payloadAttrs))
	for _, attr := range payloadAttrs {
		payloadArgs = append(payloadArgs, attr)
	}
	return slog.Group("", payloadArgs...)
}

func (l *Logger) appendEntry(level, message string, attrs ...slog.Attr) {
	buf := l.bufferOrAncestor()
	if buf == nil {
		return
	}
	buf.mu.Lock()
	buf.seq++
	entry := Entry{
		Level:   level,
		Message: message,
		At:      time.Now(),
		Seq:     buf.seq,
	}
	if len(attrs) > 0 {
		entry.Attrs = append(entry.Attrs, attrs...)
	}
	buf.entries = append(buf.entries, entry)
	buf.mu.Unlock()
}

func (l *Logger) bufferOrAncestor() *buffer {
	if l.buffer != nil {
		return l.buffer
	}
	for current := l.parent; current != nil; current = current.parent {
		if current.buffer != nil {
			return current.buffer
		}
	}
	return nil
}

func (l *Logger) collectAttrs() []slog.Attr {
	chain := []*Logger{}
	for current := l; current != nil; current = current.parent {
		chain = append(chain, current)
	}

	attrs := make([]slog.Attr, 0)
	for i := len(chain) - 1; i >= 0; i-- {
		logger := chain[i]
		logger.mu.Lock()
		if len(logger.attrs) > 0 {
			attrs = append(attrs, logger.attrs...)
		}
		logger.mu.Unlock()
	}

	return attrs
}

func entriesToPayload(entries []Entry) []map[string]any {
	payload := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		entryPayload := map[string]any{
			"message": entry.Message,
			"level":   entry.Level,
			"at":      entry.At,
			"seq":     entry.Seq,
		}
		if len(entry.Attrs) > 0 {
			attrsMap := attrsToMap(entry.Attrs)
			for key, value := range attrsMap {
				if _, exists := entryPayload[key]; exists {
					continue
				}
				entryPayload[key] = value
			}
		}
		payload = append(payload, entryPayload)
	}
	return payload
}

func attrsToMap(attrs []slog.Attr) map[string]any {
	result := map[string]any{}
	for _, attr := range attrs {
		if attr.Key == "" {
			if attr.Value.Kind() == slog.KindGroup {
				for key, value := range attrsToMap(attr.Value.Group()) {
					result[key] = value
				}
			}
			continue
		}
		result[attr.Key] = valueToAny(attr.Value)
	}
	return result
}

func valueToAny(value slog.Value) any {
	switch value.Kind() {
	case slog.KindAny:
		return value.Any()
	case slog.KindBool:
		return value.Bool()
	case slog.KindDuration:
		return value.Duration()
	case slog.KindFloat64:
		return value.Float64()
	case slog.KindInt64:
		return value.Int64()
	case slog.KindString:
		return value.String()
	case slog.KindTime:
		return value.Time()
	case slog.KindUint64:
		return value.Uint64()
	case slog.KindGroup:
		return attrsToMap(value.Group())
	default:
		return value.String()
	}
}
