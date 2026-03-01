package backends

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/messages"
)

func writePromptOK(w http.ResponseWriter, sessionID string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"info": map[string]any{
			"id":         "m1",
			"cost":       0,
			"mode":       "default",
			"modelID":    "gpt-5-mini",
			"parentID":   "",
			"path":       map[string]any{"cwd": "", "root": ""},
			"providerID": "openai",
			"role":       "assistant",
			"sessionID":  sessionID,
			"system":     []any{},
			"time":       map[string]any{"created": 0, "completed": 0},
			"tokens": map[string]any{
				"cache":     map[string]any{"read": 0, "write": 0},
				"input":     0,
				"output":    0,
				"reasoning": 0,
			},
		},
		"parts": []any{},
	})
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

func TestSendOpencodeMessage_CallsMessageEndpoint(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/session/abc/message", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want %s", r.Method, http.MethodPost)
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
		writePromptOK(w, "abc")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	backend := LocalBackend{}
	msg := &messages.Message{Parts: []messages.MessagePart{messages.NewTextPart("hello")}}
	if err := backend.sendOpencodeMessage(context.Background(), opencodeConfigFromServer(t, srv), "abc", "", "openai/gpt-5-mini", msg); err != nil {
		t.Fatalf("sendOpencodeMessage: %v", err)
	}
}

func TestSeedOpencodeMessage_CallsMessageEndpointWithNoReply(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/session/abc/message", func(w http.ResponseWriter, r *http.Request) {
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
		writePromptOK(w, "abc")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	backend := LocalBackend{}
	msg := &messages.Message{Parts: []messages.MessagePart{messages.NewTextPart("seed")}}
	if err := backend.seedOpencodeMessage(context.Background(), opencodeConfigFromServer(t, srv), "abc", "", "openai/gpt-5-mini", msg); err != nil {
		t.Fatalf("seedOpencodeMessage: %v", err)
	}
}
