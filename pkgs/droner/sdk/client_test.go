package sdk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	"github.com/Oudwins/droner/pkgs/droner/internals/schemas"
)

func TestClientVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/version" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("  test-version  "))
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL), WithHTTPClient(server.Client()))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	version, err := client.Version(ctx)
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if version != "test-version" {
		t.Fatalf("expected trimmed version, got %q", version)
	}
}

func TestClientSessionFlows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case http.MethodPost + " /sessions":
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(&schemas.SessionCreateResponse{ID: "stream-1", Harness: conf.HarnessOpenCode, Branch: schemas.NewSBranch("simple-1"), TaskID: "task1"})
		case http.MethodDelete + " /sessions":
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(&schemas.TaskResponse{TaskID: "task2", Status: schemas.TaskStatusPending, Type: "session_delete"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL), WithHTTPClient(server.Client()))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	createResp, err := client.CreateSession(ctx, schemas.SessionCreateRequest{Path: "/repo"})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	if createResp.Branch != "simple-1" {
		t.Fatalf("unexpected branch %s", createResp.Branch)
	}
	if createResp.Harness != conf.HarnessOpenCode {
		t.Fatalf("unexpected harness %s", createResp.Harness)
	}
	if createResp.TaskID != "task1" {
		t.Fatalf("unexpected task id %s", createResp.TaskID)
	}

	deleteResp, err := client.DeleteSession(ctx, schemas.SessionDeleteRequest{Branch: schemas.NewSBranch("abc")})
	if err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if deleteResp.TaskID != "task2" {
		t.Fatalf("unexpected delete task id %s", deleteResp.TaskID)
	}
}

func TestClientErrorMapping(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(ErrorResponse{Status: "failed", Code: "invalid", Message: "bad"})
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL), WithHTTPClient(server.Client()))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := client.Version(ctx)
	if err == nil {
		t.Fatalf("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest || apiErr.Code != "invalid" || !strings.Contains(apiErr.Error(), "bad") {
		t.Fatalf("unexpected api error: %+v", apiErr)
	}

	authServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(ErrorResponse{Status: "failed", Code: "auth_required", Message: "auth"})
	}))
	defer authServer.Close()

	client = NewClient(WithBaseURL(authServer.URL), WithHTTPClient(authServer.Client()))
	_, err = client.Version(ctx)
	if err == nil || err != ErrAuthRequired {
		t.Fatalf("expected ErrAuthRequired")
	}
}

func TestClientListSessionsWithParamsIncludesDirection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sessions" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		query := r.URL.Query()
		assertQueryValues(t, query, "status", []string{"queued", "active.idle"})
		if got := query.Get("limit"); got != "25" {
			t.Fatalf("limit = %q, want 25", got)
		}
		if got := query.Get("cursor"); got != "cursor-123" {
			t.Fatalf("cursor = %q, want cursor-123", got)
		}
		if got := query.Get("direction"); got != "before" {
			t.Fatalf("direction = %q, want before", got)
		}

		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(&schemas.SessionListResponse{})
	}))
	defer server.Close()

	client := NewClient(WithBaseURL(server.URL), WithHTTPClient(server.Client()))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if _, err := client.ListSessionsWithParams(ctx, []SessionStatus{SessionStatusQueued, SessionStatusActiveIdle}, 25, "cursor-123", "before"); err != nil {
		t.Fatalf("ListSessionsWithParams: %v", err)
	}
}

func assertQueryValues(t *testing.T, query url.Values, key string, want []string) {
	t.Helper()

	got := query[key]
	if len(got) != len(want) {
		t.Fatalf("%s values = %#v, want %#v", key, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s values = %#v, want %#v", key, got, want)
		}
	}
}
