package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
)

// CatalogGenre describes a feed category published by tailfeed-infra.
type CatalogGenre struct {
	Slug        string `json:"slug"`
	Label       string `json:"label"`
	LabelJA     string `json:"label_ja"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order"`
}

// CatalogFeed is a verified RSS/Atom feed published by tailfeed-infra.
type CatalogFeed struct {
	FeedID       string   `json:"feed_id"`
	URL          string   `json:"url"`
	Title        string   `json:"title"`
	Description  string   `json:"description"`
	SiteURL      string   `json:"site_url"`
	Language     string   `json:"language"`
	Genres       []string `json:"genres"`
	QualityScore int      `json:"quality_score"`
}

type catalogGenresResponse struct {
	Genres []CatalogGenre `json:"genres"`
}

type catalogFeedsResponse struct {
	Feeds  []CatalogFeed `json:"feeds"`
	Cursor string        `json:"cursor"`
}

// CatalogGenres returns the public feed catalog categories.
func CatalogGenres() ([]CatalogGenre, error) {
	var result catalogGenresResponse
	if err := catalogGET(baseURL+"/v1/catalog/genres", &result); err != nil {
		return nil, fmt.Errorf("catalog genres: %w", err)
	}
	return result.Genres, nil
}

// CatalogFeeds returns verified feeds for a category.
func CatalogFeeds(genre, language string, limit int) ([]CatalogFeed, error) {
	query := url.Values{"genre": {genre}}
	if language != "" {
		query.Set("language", language)
	}
	if limit > 0 {
		query.Set("limit", strconv.Itoa(limit))
	}
	var result catalogFeedsResponse
	if err := catalogGET(baseURL+"/v1/catalog/feeds?"+query.Encode(), &result); err != nil {
		return nil, fmt.Errorf("catalog feeds: %w", err)
	}
	return result.Feeds, nil
}

func catalogGET(endpoint string, target any) error {
	resp, err := httpClient.Get(endpoint)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}
	if err := json.Unmarshal(body, target); err != nil {
		return err
	}
	return nil
}
