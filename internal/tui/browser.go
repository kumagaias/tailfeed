package tui

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// parseSuggestJSON extracts feeds from AI response JSON.
func parseSuggestJSON(text string) ([]suggestFeed, error) {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return nil, fmt.Errorf("no JSON found")
	}
	var result struct {
		Feeds []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Description string `json:"description"`
		} `json:"feeds"`
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &result); err != nil {
		return nil, err
	}
	feeds := make([]suggestFeed, 0, len(result.Feeds))
	for _, f := range result.Feeds {
		if f.URL != "" {
			feeds = append(feeds, suggestFeed{Title: f.Title, URL: f.URL, Description: f.Description})
		}
	}
	return feeds, nil
}

func openBrowser(url string) error {
	var cmd string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
	case "linux":
		cmd = "xdg-open"
	default:
		cmd = "start"
	}
	return exec.Command(cmd, url).Start()
}
