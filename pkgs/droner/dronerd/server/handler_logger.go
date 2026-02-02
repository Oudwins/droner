package server

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

func HandlerWithLogger(handler func(*slog.Logger, http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-Id")
		if requestID == "" {
			requestID = strconv.FormatInt(time.Now().UnixNano(), 10)
		}

		logger := slog.Default().With(slog.String("request_id", requestID))
		handler(logger, w, r)
	}
}
