package server

import (
	"net/http"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/Oudwins/droner/pkgs/droner/internals/logbuf"
	"log/slog"
)

func (s *Server) MiddlewareLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-Id")
		if requestID == "" {
			requestID = strconv.FormatInt(time.Now().UnixNano(), 10)
		}

		logger := s.Logbuf.With(
			slog.String("request_id", requestID),
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
		)
		ctx := logbuf.WithContext(r.Context(), logger)
		recorder := &statusRecorder{ResponseWriter: w}
		start := time.Now()

		defer func() {
			if recovered := recover(); recovered != nil {
				_ = logger.Error("panic", slog.Any("error", recovered), slog.String("stack", string(debug.Stack())))
				if recorder.status == 0 {
					recorder.WriteHeader(http.StatusInternalServerError)
				}
			}

			status := recorder.status
			if status == 0 {
				status = http.StatusOK
			}
			logger.Add(slog.Int("status", status))
			logger.Add(slog.Duration("duration", time.Since(start)))

			payload := logger.Flush()
			s.Logger.Info("request", payload)
		}()

		_ = logger.Info("request")
		next.ServeHTTP(recorder, r.WithContext(ctx))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(p []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(p)
}
