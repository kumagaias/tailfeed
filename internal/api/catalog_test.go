package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCatalogGETDecodesResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"genres":[{"slug":"ai-ml","label":"AI & ML","label_ja":"AI・機械学習","sort_order":1}]}`))
	}))
	defer server.Close()

	var result catalogGenresResponse
	if err := catalogGET(server.URL, &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Genres) != 1 || result.Genres[0].Slug != "ai-ml" {
		t.Fatalf("unexpected genres: %#v", result.Genres)
	}
}

func TestCatalogGETRejectsErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	var result catalogGenresResponse
	if err := catalogGET(server.URL, &result); err == nil {
		t.Fatal("expected catalog error")
	}
}

func TestAPIEndpointUsesEnvironmentOverride(t *testing.T) {
	t.Setenv("TAILFEED_API_ENDPOINT", "https://api.example.com/")
	if got := apiEndpoint(); got != "https://api.example.com" {
		t.Fatalf("apiEndpoint() = %q", got)
	}
}
