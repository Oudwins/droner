package backends

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/messages"
)

func writeCommandOK(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func opencodeConfigFromServer(t *testing.T, srv *httptest.Server) conf.OpenCodeConfig {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	host, portStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatalf("split host/port: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}
	return conf.OpenCodeConfig{Hostname: host, Port: port}
}

func TestSendOpencodeMessage_CallsPromptAsyncEndpoint(t *testing.T) {
	worktreeDir := t.TempDir()
	filePath := filepath.Join(worktreeDir, "pkgs", "droner", "tui", "tui.go")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filePath, []byte("package tui\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	expectedMime := "text/plain"

	mux := http.NewServeMux()
	mux.HandleFunc("/session/abc/prompt_async", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want %s", r.Method, http.MethodPost)
		}
		if got := r.URL.Query().Get("directory"); got != worktreeDir {
			t.Fatalf("directory query = %q, want %q", got, worktreeDir)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if v, ok := body["noReply"]; ok && v != false {
			t.Fatalf("noReply = %v, want omitted or false", v)
		}
		if _, ok := body["parts"]; !ok {
			t.Fatalf("missing parts")
		}
		parts, ok := body["parts"].([]any)
		if !ok || len(parts) != 2 {
			t.Fatalf("parts = %#v, want two parts", body["parts"])
		}
		filePart, ok := parts[1].(map[string]any)
		if !ok {
			t.Fatalf("file part = %#v, want object", parts[1])
		}
		if filePart["type"] != "file" {
			t.Fatalf("file part type = %v, want file", filePart["type"])
		}
		if filePart["filename"] != "tui.go" {
			t.Fatalf("filename = %v, want tui.go", filePart["filename"])
		}
		if filePart["mime"] != expectedMime {
			t.Fatalf("mime = %v, want %v", filePart["mime"], expectedMime)
		}
		parsedURL, err := url.Parse(filePart["url"].(string))
		if err != nil {
			t.Fatalf("parse file url: %v", err)
		}
		if parsedURL.Scheme != "file" {
			t.Fatalf("file url scheme = %q, want file", parsedURL.Scheme)
		}
		if parsedURL.Path != filePath {
			t.Fatalf("file url path = %q, want %q", parsedURL.Path, filePath)
		}
		source, ok := filePart["source"].(map[string]any)
		if !ok {
			t.Fatalf("source = %#v, want object", filePart["source"])
		}
		if source["type"] != "file" || source["path"] != "pkgs/droner/tui/tui.go" {
			t.Fatalf("source = %#v", source)
		}
		text, ok := source["text"].(map[string]any)
		if !ok {
			t.Fatalf("source.text = %#v, want object", source["text"])
		}
		if text["start"] != float64(8) || text["end"] != float64(31) || text["value"] != "@pkgs/droner/tui/tui.go" {
			t.Fatalf("source.text = %#v", text)
		}
		model, ok := body["model"].(map[string]any)
		if !ok {
			t.Fatalf("missing model")
		}
		if model["providerID"] != "openai" {
			t.Fatalf("providerID = %v, want openai", model["providerID"])
		}
		if model["modelID"] != "gpt-5-mini" {
			t.Fatalf("modelID = %v, want gpt-5-mini", model["modelID"])
		}
		if body["agent"] != "plan" {
			t.Fatalf("agent = %v, want plan", body["agent"])
		}
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	backend := LocalBackend{}
	filePart := messages.NewFilePart("pkgs/droner/tui/tui.go")
	filePart.File.Source.Text = &messages.FilePartSourceTextData{Start: 8, End: 31, Value: "@pkgs/droner/tui/tui.go"}
	msg := &messages.Message{Parts: []messages.MessagePart{messages.NewTextPart("hello"), filePart}}
	if err := backend.sendOpencodeMessage(context.Background(), opencodeConfigFromServer(t, srv), "abc", worktreeDir, "openai/gpt-5-mini", "plan", msg); err != nil {
		t.Fatalf("sendOpencodeMessage: %v", err)
	}
}

func TestSeedOpencodeMessage_CallsPromptAsyncEndpointWithNoReply(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/session/abc/prompt_async", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want %s", r.Method, http.MethodPost)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["noReply"] != true {
			t.Fatalf("noReply = %v, want true", body["noReply"])
		}
		if body["agent"] != "build" {
			t.Fatalf("agent = %v, want build", body["agent"])
		}
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	backend := LocalBackend{}
	msg := &messages.Message{Parts: []messages.MessagePart{messages.NewTextPart("seed")}}
	if err := backend.seedOpencodeMessage(context.Background(), opencodeConfigFromServer(t, srv), "abc", "", "openai/gpt-5-mini", "build", msg); err != nil {
		t.Fatalf("seedOpencodeMessage: %v", err)
	}
}

func TestSendOpencodeMessage_ForwardsInlineImagePartsUnchanged(t *testing.T) {
	mux := http.NewServeMux()
	inlinePart := messages.NewDataURLFilePart("image/png", "pasted-image-1.png", "data:image/png;base64,ZmFrZQ==")
	mux.HandleFunc("/session/abc/prompt_async", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want %s", r.Method, http.MethodPost)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		parts, ok := body["parts"].([]any)
		if !ok || len(parts) != 1 {
			t.Fatalf("parts = %#v, want one part", body["parts"])
		}
		filePart, ok := parts[0].(map[string]any)
		if !ok {
			t.Fatalf("file part = %#v, want object", parts[0])
		}
		if filePart["url"] != "data:image/png;base64,ZmFrZQ==" {
			t.Fatalf("url = %v, want inline data url", filePart["url"])
		}
		if filePart["mime"] != "image/png" {
			t.Fatalf("mime = %v, want image/png", filePart["mime"])
		}
		if filePart["filename"] != "pasted-image-1.png" {
			t.Fatalf("filename = %v, want pasted-image-1.png", filePart["filename"])
		}
		if _, exists := filePart["source"]; exists {
			t.Fatalf("expected inline file part source to be omitted, got %#v", filePart["source"])
		}
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	backend := LocalBackend{}
	msg := &messages.Message{Parts: []messages.MessagePart{inlinePart}}
	if err := backend.sendOpencodeMessage(context.Background(), opencodeConfigFromServer(t, srv), "abc", "", "", "", msg); err != nil {
		t.Fatalf("sendOpencodeMessage: %v", err)
	}
}

func TestSendOpencodeCommand_CallsCommandEndpointWithAttachments(t *testing.T) {
	worktreeDir := t.TempDir()
	readmePath := filepath.Join(worktreeDir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# droner\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	inlinePart := messages.NewDataURLFilePart("image/png", "shot.png", "data:image/png;base64,ZmFrZQ==")
	mux := http.NewServeMux()
	mux.HandleFunc("/session/abc/command", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want %s", r.Method, http.MethodPost)
		}
		if got := r.URL.Query().Get("directory"); got != worktreeDir {
			t.Fatalf("directory query = %q, want %q", got, worktreeDir)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["command"] != "review" {
			t.Fatalf("command = %v, want review", body["command"])
		}
		if body["arguments"] != "README.md [Image 1]" {
			t.Fatalf("arguments = %v", body["arguments"])
		}
		if body["agent"] != "plan" {
			t.Fatalf("agent = %v, want plan", body["agent"])
		}
		if body["model"] != "openai/gpt-5-mini" {
			t.Fatalf("model = %v, want openai/gpt-5-mini", body["model"])
		}
		if _, exists := body["directory"]; exists {
			t.Fatalf("expected directory to be omitted from body, got %#v", body["directory"])
		}
		parts, ok := body["parts"].([]any)
		if !ok || len(parts) != 2 {
			t.Fatalf("parts = %#v, want two parts", body["parts"])
		}
		filePart, ok := parts[0].(map[string]any)
		if !ok {
			t.Fatalf("file part = %#v", parts[0])
		}
		if filePart["type"] != "file" || filePart["filename"] != "README.md" {
			t.Fatalf("file part = %#v", filePart)
		}
		if filePart["mime"] != "text/plain" {
			t.Fatalf("mime = %v, want text/plain", filePart["mime"])
		}
		source, ok := filePart["source"].(map[string]any)
		if !ok || source["path"] != "README.md" {
			t.Fatalf("source = %#v", filePart["source"])
		}
		text, ok := source["text"].(map[string]any)
		if !ok || text["start"] != float64(11) || text["end"] != float64(21) || text["value"] != "@README.md" {
			t.Fatalf("source.text = %#v", source["text"])
		}
		imagePart, ok := parts[1].(map[string]any)
		if !ok {
			t.Fatalf("image part = %#v", parts[1])
		}
		if imagePart["url"] != *inlinePart.File.URL {
			t.Fatalf("image url = %v, want %v", imagePart["url"], *inlinePart.File.URL)
		}
		if _, exists := imagePart["source"]; exists {
			t.Fatalf("expected inline image source to be omitted, got %#v", imagePart["source"])
		}
		writeCommandOK(w)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	backend := LocalBackend{}
	command := &messages.CommandInvocation{
		Name:      "review",
		Arguments: "README.md [Image 1]",
		Parts: []messages.MessagePart{
			func() messages.MessagePart {
				part := messages.NewFilePart("README.md")
				part.File.Source.Text = &messages.FilePartSourceTextData{Start: 11, End: 21, Value: "@README.md"}
				return part
			}(),
			inlinePart,
		},
	}
	if err := backend.sendOpencodeCommand(context.Background(), opencodeConfigFromServer(t, srv), "abc", worktreeDir, "openai/gpt-5-mini", "plan", command); err != nil {
		t.Fatalf("sendOpencodeCommand: %v", err)
	}
}

func TestOpencodePartsFromMessageRejectsMissingFile(t *testing.T) {
	t.Parallel()

	_, err := opencodePartsFromMessage(&messages.Message{Parts: []messages.MessagePart{messages.NewFilePart("missing.txt")}}, t.TempDir())
	if err == nil {
		t.Fatal("expected missing file error")
	}
}

func TestNewFilePartStartsWithNilURL(t *testing.T) {
	t.Parallel()

	part := messages.NewFilePart("pkgs/droner/tui/tui.go")
	if part.File == nil {
		t.Fatal("expected nested file payload")
	}
	if part.File.URL != nil {
		t.Fatalf("expected nil url, got %#v", part.File.URL)
	}
}
