package logbuf

import (
	"encoding/json"
	"sync"
	"time"
)

type Entry struct {
	Level   string    `json:"level"`
	Message string    `json:"message"`
	At      time.Time `json:"at"`
	Seq     uint64    `json:"seq"`
}

type Payload struct {
	ID      string      `json:"id"`
	StartAt time.Time   `json:"start_at"`
	EndAt   time.Time   `json:"end_at"`
	Data    PayloadData `json:"data"`
}

type PayloadData struct {
	Logs []Entry `json:"logs"`
}

type Logger struct {
	mu      sync.Mutex
	id      string
	startAt time.Time
	entries []Entry
	seq     uint64
}

var loggerPool = sync.Pool{
	New: func() any {
		return &Logger{}
	},
}

func New(id string) *Logger {
	logger := loggerPool.Get().(*Logger)
	logger.id = id
	logger.startAt = time.Now()
	logger.entries = logger.entries[:0]
	logger.seq = 0
	return logger
}

func (l *Logger) Emerg(message string) error {
	l.appendEntry("emerg", message)
	return nil
}

func (l *Logger) Alert(message string) error {
	l.appendEntry("alert", message)
	return nil
}

func (l *Logger) Crit(message string) error {
	l.appendEntry("crit", message)
	return nil
}

func (l *Logger) Err(message string) error {
	l.appendEntry("err", message)
	return nil
}

func (l *Logger) Warning(message string) error {
	l.appendEntry("warning", message)
	return nil
}

func (l *Logger) Notice(message string) error {
	l.appendEntry("notice", message)
	return nil
}

func (l *Logger) Info(message string) error {
	l.appendEntry("info", message)
	return nil
}

func (l *Logger) Debug(message string) error {
	l.appendEntry("debug", message)
	return nil
}

func (l *Logger) Write(p []byte) (int, error) {
	l.appendEntry("info", string(p))
	return len(p), nil
}

func (l *Logger) Flush() (string, error) {
	l.mu.Lock()
	entries := make([]Entry, len(l.entries))
	copy(entries, l.entries)
	payload := Payload{
		ID:      l.id,
		StartAt: l.startAt,
		EndAt:   time.Now(),
		Data:    PayloadData{Logs: entries},
	}
	l.resetLocked()
	l.mu.Unlock()

	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (l *Logger) appendEntry(level, message string) {
	l.mu.Lock()
	l.seq++
	l.entries = append(l.entries, Entry{
		Level:   level,
		Message: message,
		At:      time.Now(),
		Seq:     l.seq,
	})
	l.mu.Unlock()
}

func (l *Logger) resetLocked() {
	l.id = ""
	l.startAt = time.Time{}
	l.entries = l.entries[:0]
	l.seq = 0
	loggerPool.Put(l)
}
