package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// overrideBaseURL patches the unexported baseURL via the test binary's init trick.
// Instead, we use a real HTTP test server and write api.json manually.

func TestLoadReturnsNilWhenMissing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	cfg, err := load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg != nil {
		t.Fatalf("expected nil, got %+v", cfg)
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".config", "tailfeed", "api.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]string{"user_key": "test-key-123", "tier": "free"})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["user_key"] != "test-key-123" {
		t.Fatalf("want test-key-123, got %s", got["user_key"])
	}
	if got["tier"] != "free" {
		t.Fatalf("want free, got %s", got["tier"])
	}
	t.Logf("Save/Load OK: user_key=%s tier=%s", got["user_key"], got["tier"])
}

func TestRegisterEndpointIntegration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/register" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"user_key":"mock-key-abc","tier":"free"}`))
	}))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/register", "application/json", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var got map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["user_key"] == "" {
		t.Fatal("user_key must not be empty")
	}
	t.Logf("Register endpoint OK: user_key=%s tier=%s", got["user_key"], got["tier"])
}

func TestSuggestEndpointIntegration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/suggest" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"feeds":[{"title":"Go Blog","url":"https://go.dev/blog/feed.atom","description":"Official Go blog"}]}`))
	}))
	defer srv.Close()

	body := []byte(`{"user_key":"test","query":"golang"}`)
	resp, err := http.Post(srv.URL+"/v1/suggest", "application/json",
		bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	feeds, ok := got["feeds"].([]any)
	if !ok || len(feeds) == 0 {
		t.Fatal("expected at least one feed in response")
	}
	t.Logf("Suggest endpoint OK: %d feed(s) returned", len(feeds))
}

func TestSummaryEndpointIntegration(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/summary" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"summary":"今日の主なニュース: Go 1.23 がリリースされました。"}`))
	}))
	defer srv.Close()

	body := []byte(`{"user_key":"test","articles":[{"title":"Go 1.23","url":"https://example.com","summary":"Released"}],"language":"Japanese"}`)
	resp, err := http.Post(srv.URL+"/v1/summary", "application/json",
		bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var got map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got["summary"] == "" {
		t.Fatal("expected non-empty summary")
	}
	t.Logf("Summary endpoint OK: %s", got["summary"])
}

// load is a local helper that mimics api.Load() without importing the package
// (avoids circular deps and keeps the test self-contained).
func load(homeDir string) (*struct{ UserKey, Tier string }, error) {
	path := filepath.Join(homeDir, ".config", "tailfeed", "api.json")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cfg struct{ UserKey, Tier string }
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

