package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireDataToken(t *testing.T) {
	handler := requireDataToken("archive-secret", http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/data/aggregate", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", unauthorized.Code)
	}

	authorizedRequest := httptest.NewRequest(http.MethodGet, "/api/data/aggregate", nil)
	authorizedRequest.Header.Set("Authorization", "Bearer archive-secret")
	authorized := httptest.NewRecorder()
	handler.ServeHTTP(authorized, authorizedRequest)
	if authorized.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", authorized.Code)
	}
}

func TestMissingDataTokenHidesEndpoint(t *testing.T) {
	handler := requireDataToken("", http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/data/export.ndjson", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", recorder.Code)
	}
}
