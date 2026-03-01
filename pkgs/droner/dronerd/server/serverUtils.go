package server

import (
	"encoding/json"
	"net/http"
)

type JsonResponseStatus string

const (
	JsonResponseStatusSuccess JsonResponseStatus = "success"
	JsonResponseStatusFailed  JsonResponseStatus = "failed"
)

type JsonResponseErrorCode string

const (
	JsonResponseErrorCodeInvalidJson      JsonResponseErrorCode = "invalid_json"
	JsonResponseErrorCodeValidationFailed JsonResponseErrorCode = "validation_failed"
	JsonResponseErroCodeInternal          JsonResponseErrorCode = "internal"
	JsonResponseErrorCodeNotFound         JsonResponseErrorCode = "not_found"
	JsonResponseErrorCodeAuthRequired     JsonResponseErrorCode = "auth_required"
)

type ErrorResponse struct {
	Status  JsonResponseStatus    `json:"status"`
	Code    JsonResponseErrorCode `json:"code"`
	Message string                `json:"message"`
	Errors  map[string][]string   `json:"errors,omitempty"`
}

func JsonResponseError(code JsonResponseErrorCode, message string, errors map[string][]string) *ErrorResponse {
	return &ErrorResponse{
		Status:  JsonResponseStatusFailed,
		Code:    code,
		Message: message,
		Errors:  errors,
	}
}

type RenderOption = func(w http.ResponseWriter, r *http.Request)

type Renderer struct {
}

func (r *Renderer) Status(status int) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
	}
}

var Render = Renderer{}

func RenderJSON(w http.ResponseWriter, r *http.Request, payload any, opts ...RenderOption) {
	w.Header().Set("Content-Type", "application/json")
	for _, opt := range opts {
		opt(w, r)
	}
	_ = json.NewEncoder(w).Encode(payload)
}
