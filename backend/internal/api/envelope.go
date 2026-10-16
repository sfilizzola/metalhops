package api

import (
	"encoding/json"
	"net/http"
)

type dataBody struct {
	Data any `json:"data"`
}

type listBody struct {
	Data any  `json:"data"`
	Meta meta `json:"meta"`
}

type meta struct {
	NextCursor *string `json:"next_cursor"`
}

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code      string            `json:"code"`
	Message   string            `json:"message"`
	RequestID string            `json:"request_id"`
	Fields    map[string]string `json:"fields,omitempty"`
	Details   map[string]any    `json:"details,omitempty"`
}

func WriteData(w http.ResponseWriter, status int, value any) {
	writeJSON(w, status, dataBody{Data: value})
}

func WriteList(w http.ResponseWriter, status int, items any, nextCursor *string) {
	writeJSON(w, status, listBody{Data: items, Meta: meta{NextCursor: nextCursor}})
}

func WriteError(w http.ResponseWriter, status int, code, message, requestID string, fields map[string]string) {
	writeJSON(w, status, errorBody{Error: errorDetail{
		Code: code, Message: message, RequestID: requestID, Fields: fields,
	}})
}

func WriteErrorDetails(w http.ResponseWriter, status int, code, message, requestID string, fields map[string]string, details map[string]any) {
	writeJSON(w, status, errorBody{Error: errorDetail{
		Code: code, Message: message, RequestID: requestID, Fields: fields, Details: details,
	}})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
