package server

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Oudwins/droner/pkgs/droner/internals/logbuf"
)

func TestMiddlewareStatusRecorder(t *testing.T) {
	recorder := httptest.NewRecorder()
	sr := &statusRecorder{ResponseWriter: recorder}

	_, _ = sr.Write([]byte("ok"))
	if sr.status != http.StatusOK {
		t.Fatalf("expected status 200, got %d", sr.status)
	}

	recorder = httptest.NewRecorder()
	sr = &statusRecorder{ResponseWriter: recorder}
	sr.WriteHeader(http.StatusNotFound)
	if sr.status != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", sr.status)
	}
}

func TestMiddlewareLoggerPanic(t *testing.T) {
	s := &Server{
		Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Logbuf: logbuf.New(),
	}

	handler := s.MiddlewareLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/version", nil)
	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", recorder.Code)
	}
}
