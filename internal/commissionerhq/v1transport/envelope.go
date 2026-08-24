package v1transport

import (
	"encoding/json"
	"net/http"
)

type ErrorEnvelope struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

type envelopeDefinition struct {
	code    string
	message string
}

var envelopeDefinitions = map[int]envelopeDefinition{
	http.StatusBadRequest:          {"invalid_request", "Request was invalid"},
	http.StatusUnauthorized:        {"unauthorized", "Request could not be authorized"},
	http.StatusForbidden:           {"forbidden", "Access is not permitted"},
	http.StatusMethodNotAllowed:    {"method_not_allowed", "Method is not allowed"},
	http.StatusTooManyRequests:     {"rate_limited", "Retry later"},
	http.StatusInternalServerError: {"internal_error", "Internal error"},
	http.StatusServiceUnavailable:  {"temporarily_unavailable", "League data is temporarily unavailable"},
}

func EnvelopeForStatus(status int, requestID string) (ErrorEnvelope, bool) {
	definition, ok := envelopeDefinitions[status]
	if !ok {
		return ErrorEnvelope{}, false
	}
	return ErrorEnvelope{Error: ErrorDetail{
		Code: definition.code, Message: definition.message, RequestID: safeRequestID(requestID),
	}}, true
}

func writeEnvelope(writer http.ResponseWriter, status int, requestID string) {
	envelope, ok := EnvelopeForStatus(status, requestID)
	if !ok {
		status = http.StatusInternalServerError
		envelope, _ = EnvelopeForStatus(status, requestID)
	}
	setPrivateJSONHeaders(writer.Header(), envelope.Error.RequestID)
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(envelope)
}

func setPrivateJSONHeaders(header http.Header, requestID string) {
	header.Set("Cache-Control", "private, no-store")
	header.Set("Content-Type", "application/json; charset=utf-8")
	header.Set(HeaderRequestID, safeRequestID(requestID))
}
