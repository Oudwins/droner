package sdk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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

func TestClientTaskFlows(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case http.MethodPost + " /sessions":
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(&schemas.TaskResponse{TaskID: "task1", Status: schemas.TaskStatusPending, Type: "session_create"})
		case http.MethodDelete + " /sessions":
			w.WriteHeader(http.StatusAccepted)
			_ = json.NewEncoder(w).Encode(&schemas.TaskResponse{TaskID: "task2", Status: schemas.TaskStatusPending, Type: "session_delete"})
		case http.MethodGet + " /tasks/task1":
			_ = json.NewEncoder(w).Encode(&schemas.TaskResponse{TaskID: "task1", Status: schemas.TaskStatusSucceeded, Type: "session_create"})
		case http.MethodGet + " /tasks/task2":
			_ = json.NewEncoder(w).Encode(&schemas.TaskResponse{TaskID: "task2", Status: schemas.TaskStatusSucceeded, Type: "session_delete"})
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
	if createResp.TaskID != "task1" {
		t.Fatalf("unexpected task id %s", createResp.TaskID)
	}

	statusResp, err := client.TaskStatus(ctx, "task1")
	if err != nil {
		t.Fatalf("TaskStatus: %v", err)
	}
	if statusResp.Status != schemas.TaskStatusSucceeded {
		t.Fatalf("expected succeeded, got %s", statusResp.Status)
	}

	deleteResp, err := client.DeleteSession(ctx, schemas.SessionDeleteRequest{SessionID: schemas.NewSSessionID("abc")})
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
