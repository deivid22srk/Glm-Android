package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSAllowsPrivateNetworkPreflight(t *testing.T) {
	handler := (&Server{}).cors(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("preflight should not reach wrapped handler")
	}))

	request := httptest.NewRequest(http.MethodOptions, "/api/admin/accounts?quota=0", nil)
	request.Header.Set("Origin", "wails://wails.localhost")
	request.Header.Set("Access-Control-Request-Method", http.MethodGet)
	request.Header.Set("Access-Control-Request-Headers", "content-type")
	request.Header.Set("Access-Control-Request-Private-Network", "true")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusNoContent {
		t.Fatalf("expected %d, got %d", http.StatusNoContent, response.Code)
	}
	if got := response.Header().Get("Access-Control-Allow-Private-Network"); got != "true" {
		t.Fatalf("expected private network CORS header, got %q", got)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("expected wildcard CORS origin, got %q", got)
	}
}
