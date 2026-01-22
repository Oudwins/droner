package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"droner/internals/core"
	"droner/internals/logbuf"
	"droner/internals/schemas"
)

func (s *Server) HandlerVersion(w http.ResponseWriter, r *http.Request) {
	logger := logbuf.FromContext(r.Context())
	if logger != nil {
		_ = logger.Info("version request")
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(s.Config.VERSION))
}

func (s *Server) HandlerSum(w http.ResponseWriter, r *http.Request) {
	logger := logbuf.FromContext(r.Context())
	if logger != nil {
		_ = logger.Info("sum request")
	}
	aValue := r.URL.Query().Get("a")
	bValue := r.URL.Query().Get("b")

	a, err := strconv.Atoi(aValue)
	if err != nil {
		if logger != nil {
			_ = logger.Warn("invalid a")
		}
		http.Error(w, "invalid a", http.StatusBadRequest)
		return
	}

	b, err := strconv.Atoi(bValue)
	if err != nil {
		if logger != nil {
			_ = logger.Warn("invalid b")
		}
		http.Error(w, "invalid b", http.StatusBadRequest)
		return
	}

	request := schemas.SumRequest{A: a, B: b}
	if err := schemas.ValidateSumRequest(request); err != nil {
		if logger != nil {
			_ = logger.Warn(err.Error())
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	response := schemas.SumResponse{Sum: core.Sum(request.A, request.B)}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
	if logger != nil {
		_ = logger.Info("sum response sent")
	}
}
