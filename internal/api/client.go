package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const baseURL = "https://3734qg8c00.execute-api.ap-northeast-1.amazonaws.com"

var httpClient = &http.Client{Timeout: 30 * time.Second}

// LoadOrRegister loads the saved API config, or registers automatically if not present.
// This lets free users use suggest/summary without running `tailfeed register` first.
func LoadOrRegister() (*Config, error) {
	cfg, err := Load()
	if err != nil {
		return nil, err
	}
	if cfg != nil {
		return cfg, nil
	}
	cfg, err = Register()
	if err != nil {
		return nil, err
	}
	if err := Save(cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

// Register calls POST /v1/register and returns the user config.
// If the IP is already registered the existing key is returned (idempotent).
func Register() (*Config, error) {
	resp, err := httpClient.Post(baseURL+"/v1/register", "application/json", http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("register: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 && resp.StatusCode != 201 {
		return nil, fmt.Errorf("register: server returned %d: %s", resp.StatusCode, string(body))
	}
	var cfg Config
	if err := json.Unmarshal(body, &cfg); err != nil {
		return nil, fmt.Errorf("register: %w", err)
	}
	return &cfg, nil
}

// SuggestFeed is a feed candidate returned by /v1/suggest.
type SuggestFeed struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

type suggestRequest struct {
	UserKey        string `json:"user_key"`
	ArticleTitle   string `json:"article_title,omitempty"`
	ArticleURL     string `json:"article_url,omitempty"`
	ArticleSummary string `json:"article_summary,omitempty"`
	Query          string `json:"query,omitempty"`
}

type suggestResponse struct {
	Feeds []SuggestFeed `json:"feeds"`
}

// Suggest calls POST /v1/suggest. Pass either article fields or a query string.
func Suggest(userKey, articleTitle, articleURL, articleSummary, query string) ([]SuggestFeed, error) {
	req := suggestRequest{
		UserKey:        userKey,
		ArticleTitle:   articleTitle,
		ArticleURL:     articleURL,
		ArticleSummary: articleSummary,
		Query:          query,
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Post(baseURL+"/v1/suggest", "application/json", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("suggest: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("suggest: server returned %d: %s", resp.StatusCode, string(body))
	}
	var r suggestResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("suggest: %w", err)
	}
	return r.Feeds, nil
}

// SummaryArticle is the article shape expected by /v1/summary.
type SummaryArticle struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Summary string `json:"summary"`
}

type summaryRequest struct {
	UserKey  string           `json:"user_key"`
	Articles []SummaryArticle `json:"articles"`
	Language string           `json:"language,omitempty"`
}

type summaryResponse struct {
	Summary string `json:"summary"`
}

// UsageInfo holds plan and remaining-call information returned by /v1/usage.
type UsageInfo struct {
	Plan              string `json:"plan"`
	SummaryRemaining  int    `json:"summary_remaining"`
	SummaryLimit      int    `json:"summary_limit"`
	SuggestRemaining  int    `json:"suggest_remaining"`
	SuggestLimit      int    `json:"suggest_limit"`
	ResetAt           string `json:"reset_at"` // RFC3339 date, e.g. "2026-04-18"
}

// Usage calls GET /v1/usage and returns the current plan and remaining quota.
func Usage(userKey string) (*UsageInfo, error) {
	req, err := http.NewRequest(http.MethodGet, baseURL+"/v1/usage?user_key="+userKey, nil)
	if err != nil {
		return nil, fmt.Errorf("usage: %w", err)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("usage: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("usage: server returned %d: %s", resp.StatusCode, string(body))
	}
	var info UsageInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("usage: %w", err)
	}
	return &info, nil
}

// Summary calls POST /v1/summary and returns the generated summary text.
func Summary(userKey string, articles []SummaryArticle, language string) (string, error) {
	req := summaryRequest{
		UserKey:  userKey,
		Articles: articles,
		Language: language,
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	resp, err := httpClient.Post(baseURL+"/v1/summary", "application/json", bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("summary: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("summary: server returned %d: %s", resp.StatusCode, string(body))
	}
	var r summaryResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return "", fmt.Errorf("summary: %w", err)
	}
	return r.Summary, nil
}
