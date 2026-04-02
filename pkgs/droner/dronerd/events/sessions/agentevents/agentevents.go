package agentevents

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
)

type State string

const (
	StateBusy State = "busy"
	StateIdle State = "idle"
)

type Event struct {
	WorktreePath string
	State        State
	OccurredAt   time.Time
}

type globalEventEnvelope struct {
	Directory string                 `json:"directory"`
	Payload   globalEventPayloadBody `json:"payload"`
}

type globalEventPayloadBody struct {
	Type       string          `json:"type"`
	Properties json.RawMessage `json:"properties"`
}

type sessionStatusProperties struct {
	SessionID string `json:"sessionID"`
	Status    struct {
		Type string `json:"type"`
	} `json:"status"`
}

func RunOpenCode(ctx context.Context, logger *slog.Logger, config conf.OpenCodeConfig, handle func(context.Context, Event) error) error {
	if handle == nil {
		return nil
	}
	client := &http.Client{}
	backoff := 200 * time.Millisecond
	for {
		err := streamOpenCodeEvents(ctx, client, config, handle)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil && !errors.Is(err, context.Canceled) && logger != nil {
			logger.Warn("opencode agent event stream failed", slog.String("error", err.Error()))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 3*time.Second {
			backoff *= 2
		}
	}
}

func streamOpenCodeEvents(ctx context.Context, client *http.Client, config conf.OpenCodeConfig, handle func(context.Context, Event) error) error {
	endpoint := fmt.Sprintf("http://%s:%d/global/event", config.Hostname, config.Port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
		if len(body) == 0 {
			return fmt.Errorf("opencode global event stream failed: %s", resp.Status)
		}
		return fmt.Errorf("opencode global event stream failed: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return consumeSSE(resp.Body, func(data string) error {
		evt, ok, err := parseOpenCodeEvent(data)
		if err != nil || !ok {
			return err
		}
		return handle(ctx, evt)
	})
}

func consumeSSE(r io.Reader, handle func(data string) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	dataLines := make([]string, 0, 4)
	flush := func() error {
		if len(dataLines) == 0 {
			return nil
		}
		data := strings.Join(dataLines, "\n")
		dataLines = dataLines[:0]
		return handle(data)
	}
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return flush()
}

func parseOpenCodeEvent(data string) (Event, bool, error) {
	var envelope globalEventEnvelope
	if err := json.Unmarshal([]byte(data), &envelope); err != nil {
		return Event{}, false, err
	}
	worktreePath := strings.TrimSpace(envelope.Directory)
	if worktreePath == "" || worktreePath == "global" {
		return Event{}, false, nil
	}
	worktreePath = filepath.Clean(worktreePath)
	switch envelope.Payload.Type {
	case "server.connected", "server.heartbeat":
		return Event{}, false, nil
	case "session.idle":
		return Event{WorktreePath: worktreePath, State: StateIdle, OccurredAt: time.Now().UTC()}, true, nil
	case "session.status":
		var props sessionStatusProperties
		if err := json.Unmarshal(envelope.Payload.Properties, &props); err != nil {
			return Event{}, false, err
		}
		switch props.Status.Type {
		case string(StateBusy):
			return Event{WorktreePath: worktreePath, State: StateBusy, OccurredAt: time.Now().UTC()}, true, nil
		case string(StateIdle):
			return Event{WorktreePath: worktreePath, State: StateIdle, OccurredAt: time.Now().UTC()}, true, nil
		default:
			return Event{}, false, nil
		}
	default:
		return Event{}, false, nil
	}
}
