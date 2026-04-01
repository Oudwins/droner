package eventdebug

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type ServerOptions struct {
	Title              string
	DefaultListLimit   int
	DefaultStreamLimit int
}

type Server struct {
	store              Store
	title              string
	defaultListLimit   int
	defaultStreamLimit int
	tmpl               *template.Template
	mux                *http.ServeMux
}

func NewServer(store Store, opts ServerOptions) *Server {
	title := strings.TrimSpace(opts.Title)
	if title == "" {
		title = "Droner Event Debug"
	}
	listLimit := opts.DefaultListLimit
	if listLimit <= 0 {
		listLimit = 100
	}
	streamLimit := opts.DefaultStreamLimit
	if streamLimit <= 0 {
		streamLimit = 500
	}

	s := &Server{
		store:              store,
		title:              title,
		defaultListLimit:   listLimit,
		defaultStreamLimit: streamLimit,
		tmpl: template.Must(template.New("page").Funcs(template.FuncMap{
			"pathEscape":              url.PathEscape,
			"prettyJSON":              prettyJSON,
			"fmtTime":                 fmtTime,
			"fmtElapsedBetween":       fmtElapsedBetween,
			"fmtElapsedSincePrevious": fmtElapsedSincePrevious,
		}).Parse(pageTemplate)),
		mux: http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func ListenAndServe(ctx context.Context, addr string, store Store, opts ServerOptions) error {
	server := &http.Server{
		Addr:    addr,
		Handler: NewServer(store, opts),
	}
	errCh := make(chan error, 1)
	go func() {
		err := server.ListenAndServe()
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
		<-errCh
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

func (s *Server) routes() {
	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.HandleFunc("/streams/", s.handleStreamPage)
	s.mux.HandleFunc("/api/streams", s.handleListAPI)
	s.mux.HandleFunc("/api/streams/", s.handleStreamAPI)
	s.mux.HandleFunc("/healthz", s.handleHealth)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	limit := parseLimit(r, s.defaultListLimit)
	streams, err := s.store.ListStreams(r.Context(), ListOptions{
		Query: r.URL.Query().Get("q"),
		Limit: limit,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("failed to list streams: %v", err), http.StatusInternalServerError)
		return
	}

	var selected *Stream
	selectedGroups := make([]eventGroupView, 0)
	selectedID := strings.TrimSpace(r.URL.Query().Get("stream"))
	if selectedID != "" {
		stream, err := s.store.LoadStream(r.Context(), selectedID, StreamOptions{Limit: parseStreamLimit(r, s.defaultStreamLimit)})
		if err != nil && !errors.Is(err, ErrStreamNotFound) {
			http.Error(w, fmt.Sprintf("failed to load stream: %v", err), http.StatusInternalServerError)
			return
		}
		if err == nil {
			selected = &stream
			selectedGroups = buildEventGroups(stream.Events)
		}
	}

	data := pageData{
		Title:          s.title,
		Query:          r.URL.Query().Get("q"),
		Streams:        streams,
		Selected:       selected,
		SelectedGroups: selectedGroups,
		SelectedStream: selectedID,
		ListLimit:      limit,
		StreamLimit:    parseStreamLimit(r, s.defaultStreamLimit),
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.tmpl.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("failed to render page: %v", err), http.StatusInternalServerError)
	}
}

func (s *Server) handleStreamPage(w http.ResponseWriter, r *http.Request) {
	streamID, ok := trimPathPrefix(r.URL.Path, "/streams/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	q.Set("stream", streamID)
	http.Redirect(w, r, "/?"+q.Encode(), http.StatusFound)
}

func (s *Server) handleListAPI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/streams" {
		http.NotFound(w, r)
		return
	}
	streams, err := s.store.ListStreams(r.Context(), ListOptions{
		Query: r.URL.Query().Get("q"),
		Limit: parseLimit(r, s.defaultListLimit),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"streams": streams})
}

func (s *Server) handleStreamAPI(w http.ResponseWriter, r *http.Request) {
	streamID, ok := trimPathPrefix(r.URL.Path, "/api/streams/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	stream, err := s.store.LoadStream(r.Context(), streamID, StreamOptions{Limit: parseStreamLimit(r, s.defaultStreamLimit)})
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, ErrStreamNotFound) {
			status = http.StatusNotFound
		}
		writeJSON(w, status, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, stream)
}

func trimPathPrefix(path string, prefix string) (string, bool) {
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	trimmed := strings.Trim(strings.TrimPrefix(path, prefix), "/")
	if trimmed == "" {
		return "", false
	}
	decoded, err := url.PathUnescape(trimmed)
	if err != nil {
		return "", false
	}
	return decoded, true
}

func parseLimit(r *http.Request, fallback int) int {
	return parsePositiveInt(r.URL.Query().Get("limit"), fallback)
}

func parseStreamLimit(r *http.Request, fallback int) int {
	return parsePositiveInt(r.URL.Query().Get("stream_limit"), fallback)
}

func parsePositiveInt(raw string, fallback int) int {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return fallback
	}
	value, err := strconv.Atoi(trimmed)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func prettyJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "null"
	}
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return string(raw)
	}
	return out.String()
}

func fmtTime(ts time.Time) string {
	if ts.IsZero() {
		return "-"
	}
	return ts.UTC().Format(time.RFC3339Nano)
}

func fmtElapsedSincePrevious(index int, events []Event) string {
	if index <= 0 || index >= len(events) {
		return ""
	}
	delta := events[index].OccurredAt.Sub(events[index-1].OccurredAt)
	return fmt.Sprintf("took %s", ceilDurationMilliseconds(delta))
}

func fmtElapsedBetween(start time.Time, end time.Time) string {
	if start.IsZero() || end.IsZero() {
		return "0ms"
	}
	return ceilDurationMilliseconds(end.Sub(start))
}

func ceilDurationMilliseconds(delta time.Duration) string {
	if delta <= 0 {
		return "0ms"
	}
	milliseconds := delta / time.Millisecond
	if delta%time.Millisecond != 0 {
		milliseconds++
	}
	return fmt.Sprintf("%dms", milliseconds)
}

func buildEventGroups(events []Event) []eventGroupView {
	if len(events) == 0 {
		return nil
	}

	groups := make([]eventGroupView, 0)
	for i, event := range events {
		action, status := eventActionParts(event.EventType)
		view := eventView{Event: event}
		if i > 0 {
			view.ElapsedSincePrevious = fmtElapsedSincePrevious(i, events)
		}

		if len(groups) == 0 || groups[len(groups)-1].Action != action {
			groups = append(groups, eventGroupView{Action: action})
		}

		group := &groups[len(groups)-1]
		group.Events = append(group.Events, view)
		group.EventCount = len(group.Events)
		group.Duration = fmtElapsedBetween(group.Events[0].Event.OccurredAt, event.OccurredAt)
		if status != "" {
			group.Statuses = append(group.Statuses, status)
		}
	}

	return groups
}

func eventActionParts(eventType string) (string, string) {
	trimmed := strings.TrimSpace(eventType)
	if trimmed == "" {
		return "unknown", ""
	}
	lastDot := strings.LastIndex(trimmed, ".")
	if lastDot <= 0 || lastDot == len(trimmed)-1 {
		return trimmed, ""
	}
	return trimmed[:lastDot], trimmed[lastDot+1:]
}

type pageData struct {
	Title          string
	Query          string
	Streams        []StreamSummary
	Selected       *Stream
	SelectedGroups []eventGroupView
	SelectedStream string
	ListLimit      int
	StreamLimit    int
}

type eventView struct {
	Event                Event
	ElapsedSincePrevious string
}

type eventGroupView struct {
	Action     string
	Statuses   []string
	Events     []eventView
	EventCount int
	Duration   string
}

const pageTemplate = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{.Title}}</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f4f1ea;
      --panel: #fffdf8;
      --panel-2: #f8f5ee;
      --text: #1f1d1a;
      --muted: #6a655d;
      --line: #d9d1c3;
      --accent: #0c6a60;
      --accent-soft: #dff2ee;
      --code: #1c2428;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: "Iosevka Web", "SFMono-Regular", "Consolas", monospace;
      color: var(--text);
      background:
        radial-gradient(circle at top left, #fff6de 0, transparent 28%),
        linear-gradient(180deg, #f8f3ea 0%, #f0ebe2 100%);
    }
    a { color: inherit; }
    .layout {
      display: grid;
      grid-template-columns: 320px minmax(0, 1fr);
      min-height: 100vh;
    }
    .sidebar {
      border-right: 1px solid var(--line);
      background: rgba(255, 253, 248, 0.88);
      backdrop-filter: blur(6px);
      padding: 20px;
      position: sticky;
      top: 0;
      height: 100vh;
      overflow: auto;
    }
    .content {
      padding: 24px;
    }
    h1, h2, h3, p { margin-top: 0; }
    h1 { font-size: 18px; margin-bottom: 8px; }
    .muted { color: var(--muted); }
    .search {
      display: grid;
      gap: 8px;
      margin: 16px 0 20px;
    }
    input {
      width: 100%;
      border: 1px solid var(--line);
      border-radius: 10px;
      padding: 10px 12px;
      font: inherit;
      background: var(--panel);
      color: var(--text);
    }
    button {
      border: 0;
      border-radius: 10px;
      padding: 10px 12px;
      font: inherit;
      cursor: pointer;
      background: var(--accent);
      color: white;
    }
    .stream-list {
      display: grid;
      gap: 10px;
    }
    .stream-card {
      display: block;
      text-decoration: none;
      border: 1px solid var(--line);
      border-radius: 14px;
      padding: 12px;
      background: var(--panel);
    }
    .stream-card.active {
      border-color: var(--accent);
      background: var(--accent-soft);
    }
    .stream-id {
      font-size: 13px;
      word-break: break-word;
      margin-bottom: 8px;
    }
    .stream-meta {
      font-size: 12px;
      color: var(--muted);
    }
    .hero {
      margin-bottom: 18px;
      padding: 18px;
      border: 1px solid var(--line);
      border-radius: 18px;
      background: rgba(255, 253, 248, 0.84);
    }
    .events {
      display: grid;
      gap: 14px;
    }
    .event-group {
      border: 1px solid var(--line);
      border-radius: 16px;
      background: var(--panel);
      overflow: hidden;
    }
    .event-group-summary {
      display: flex;
      justify-content: space-between;
      align-items: flex-start;
      gap: 16px;
      padding: 12px 14px;
      cursor: pointer;
      list-style: none;
      background: var(--panel-2);
    }
    .event-group-summary::-webkit-details-marker {
      display: none;
    }
    .event-group-body {
      display: grid;
      gap: 12px;
      padding: 12px;
      border-top: 1px solid var(--line);
      background: rgba(255, 253, 248, 0.5);
    }
    .event {
      border: 1px solid var(--line);
      border-radius: 16px;
      overflow: hidden;
      background: var(--panel);
    }
    .event-head {
      display: flex;
      justify-content: space-between;
      align-items: flex-start;
      gap: 16px;
      padding: 12px 14px;
      background: var(--panel-2);
      border-bottom: 1px solid var(--line);
    }
    .event-head-main,
    .event-head-side {
      display: grid;
      gap: 2px;
      align-content: start;
    }
    .event-head-side {
      justify-items: end;
      text-align: right;
    }
    .event-type { font-weight: 700; }
    .event-body {
      padding: 14px;
      display: grid;
      gap: 12px;
    }
    .kv {
      display: grid;
      grid-template-columns: repeat(auto-fit, minmax(180px, 1fr));
      gap: 10px;
      font-size: 12px;
    }
    .kv div {
      padding: 10px;
      border-radius: 10px;
      background: #fcfaf5;
      border: 1px solid #ece4d6;
    }
    pre {
      margin: 0;
      overflow: auto;
      padding: 14px;
      border-radius: 12px;
      background: var(--code);
      color: #eff8f6;
      font-size: 12px;
      line-height: 1.45;
    }
    .empty {
      padding: 28px;
      border: 1px dashed var(--line);
      border-radius: 16px;
      background: rgba(255, 253, 248, 0.75);
    }
    @media (max-width: 900px) {
      .layout { grid-template-columns: 1fr; }
      .sidebar {
        position: static;
        height: auto;
        border-right: 0;
        border-bottom: 1px solid var(--line);
      }
    }
  </style>
</head>
<body>
  <div class="layout">
    <aside class="sidebar">
      <h1>{{.Title}}</h1>
      <p class="muted">Internal event stream browser for debugging and replay inspection.</p>
      <form class="search" method="get" action="/">
        <input type="text" name="q" value="{{.Query}}" placeholder="search stream id">
        <input type="text" name="limit" value="{{.ListLimit}}" placeholder="list limit">
        <input type="text" name="stream_limit" value="{{.StreamLimit}}" placeholder="event limit">
        {{if .SelectedStream}}<input type="hidden" name="stream" value="{{.SelectedStream}}">{{end}}
        <button type="submit">Refresh</button>
      </form>
      <div class="stream-list">
        {{if .Streams}}
          {{range .Streams}}
            <a class="stream-card {{if eq $.SelectedStream .StreamID}}active{{end}}" href="/?q={{$.Query}}&limit={{$.ListLimit}}&stream_limit={{$.StreamLimit}}&stream={{pathEscape .StreamID}}">
              <div class="stream-id">{{.StreamID}}</div>
              <div class="stream-meta">{{.EventCount}} events</div>
              <div class="stream-meta">first {{fmtTime .FirstOccurredAt}}</div>
              <div class="stream-meta">last {{fmtTime .LastOccurredAt}}</div>
            </a>
          {{end}}
        {{else}}
          <div class="empty">No streams matched the current filter.</div>
        {{end}}
      </div>
    </aside>
    <main class="content">
      {{if .Selected}}
        <section class="hero">
          <h2>{{.Selected.Summary.StreamID}}</h2>
          <p class="muted">{{.Selected.Summary.EventCount}} total events. {{fmtElapsedBetween .Selected.Summary.FirstOccurredAt .Selected.Summary.LastOccurredAt}} total. Showing up to {{$.StreamLimit}}.</p>
          <p class="muted">first {{fmtTime .Selected.Summary.FirstOccurredAt}} | last {{fmtTime .Selected.Summary.LastOccurredAt}}</p>
        </section>
        <section class="events">
          {{range .SelectedGroups}}
            <details class="event-group">
              <summary class="event-group-summary">
                <div class="event-head-main">
                  <div class="event-type">{{.Action}}</div>
                  <div class="muted">{{if .Statuses}}{{range $i, $status := .Statuses}}{{if $i}} / {{end}}{{$status}}{{end}}{{else}}single event{{end}}</div>
                </div>
                <div class="event-head-side">
                  <div class="muted">{{.EventCount}} events</div>
                  <div class="muted">{{.Duration}} total</div>
                </div>
              </summary>
              <div class="event-group-body">
                {{range .Events}}
                  {{$event := .Event}}
                  <article class="event">
                    <div class="event-head">
                      <div class="event-head-main">
                        <div class="event-type">v{{$event.StreamVersion}} {{$event.EventType}}</div>
                        <div class="muted">{{fmtTime $event.OccurredAt}}</div>
                      </div>
                      <div class="event-head-side">
                        <div class="muted">schema v{{$event.SchemaVersion}}</div>
                        {{with .ElapsedSincePrevious}}<div class="muted">{{.}}</div>{{end}}
                      </div>
                    </div>
                    <div class="event-body">
                      <div class="kv">
                        <div><strong>event id</strong><br>{{$event.ID}}</div>
                        <div><strong>causation</strong><br>{{if $event.CausationID}}{{$event.CausationID}}{{else}}-{{end}}</div>
                        <div><strong>correlation</strong><br>{{if $event.CorrelationID}}{{$event.CorrelationID}}{{else}}-{{end}}</div>
                      </div>
                      <pre>{{prettyJSON $event.Payload}}</pre>
                    </div>
                  </article>
                {{end}}
              </div>
            </details>
          {{end}}
        </section>
      {{else}}
        <div class="empty">
          <h2>No stream selected</h2>
          <p class="muted">Pick a stream from the left to inspect its event history.</p>
        </div>
      {{end}}
    </main>
  </div>
</body>
</html>`
