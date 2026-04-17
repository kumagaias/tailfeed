package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kumagaias/tailfeed/internal/api"
	"github.com/kumagaias/tailfeed/internal/mcp"
)

// mcpResultMsg carries the result of an async MCP tools/call.
type mcpResultMsg struct{ text string }

// usageMsg carries the result of an async /v1/usage call.
type usageMsg struct{ text string }

// summaryHTMLMsg carries summary text and the generated HTML file path.
type summaryHTMLMsg struct {
	text string
	path string
}

// cmdSummaryToday summarises all articles published today via MCP or the tailfeed API.
func (m *Model) cmdSummaryToday() (string, tea.Cmd) {
	articles, err := m.db.ListTodayArticles()
	if err != nil {
		return "summary today: " + err.Error(), nil
	}
	if len(articles) == 0 {
		return "summary today: no articles today", nil
	}

	mcpCfg, err := mcp.Load()
	if err != nil {
		return "mcp: " + err.Error(), nil
	}
	if mcpCfg != nil {
		var sb strings.Builder
		for _, a := range articles {
			sb.WriteString(fmt.Sprintf("## %s\nURL: %s\n%s\n\n", a.Title, a.Link, a.Summary))
		}
		args := map[string]any{
			"question": fmt.Sprintf(`You are a senior engineer's daily briefing assistant. Summarize today's %d articles in %s for a technical audience. For each article: one-line TL;DR, key technical points as bullet list. End with a "## Today's Signal" section: 2-3 sentences on trends worth watching. Be concise, skip fluff.`, len(articles), mcpCfg.SummaryLanguage()),
			"context":  sb.String(),
		}
		return fmt.Sprintf("summarising %d articles…", len(articles)), func() tea.Msg {
			text, err := mcp.Call(mcpCfg, args)
			if err != nil {
				return mcpResultMsg{text: "summary today error: " + err.Error()}
			}
			path, htmlErr := writeSummaryHTML(text, articles)
			if htmlErr == nil {
				return summaryHTMLMsg{text: text, path: path}
			}
			return mcpResultMsg{text: text}
		}
	}

	apiArticles := make([]api.SummaryArticle, len(articles))
	for i, a := range articles {
		apiArticles[i] = api.SummaryArticle{Title: a.Title, URL: a.Link, Summary: a.Summary}
	}
	return fmt.Sprintf("summarising %d articles…", len(articles)), func() tea.Msg {
		apiCfg, err := api.LoadOrRegister()
		if err != nil {
			return mcpResultMsg{text: "api: " + err.Error()}
		}
		text, err := api.Summary(apiCfg.UserKey, apiArticles, "Japanese")
		if err != nil {
			return mcpResultMsg{text: "summary today error: " + err.Error()}
		}
		path, htmlErr := writeSummaryHTML(text, articles)
		if htmlErr == nil {
			return summaryHTMLMsg{text: text, path: path}
		}
		return mcpResultMsg{text: text}
	}
}

// cmdMCP runs toolName against the configured MCP server, falling back to the tailfeed API.
func (m *Model) cmdMCP(cmdName string, _ []string) (string, tea.Cmd) {
	if m.cursor >= len(m.articles) {
		return "no article selected", nil
	}
	a := m.articles[m.cursor]

	mcpCfg, err := mcp.Load()
	if err != nil {
		return "mcp: " + err.Error(), nil
	}
	if mcpCfg != nil {
		if cmdName == "suggest" {
			args := map[string]any{
				"question": `You are a feed curator for engineers. Based on the article below, suggest 20 RSS feeds a senior developer would actually subscribe to — think official blogs, release notes, technical deep-dives, not generic news. Return ONLY valid JSON, no prose:
{"feeds":[{"title":"Feed Name","url":"https://...","description":"one-line description"},{"title":"Feed Name","url":"https://...","description":"one-line description"}]}`,
				"context": fmt.Sprintf("タイトル: %s\nURL: %s\n\n%s", a.Title, a.Link, a.Summary),
			}
			return "suggesting feeds…", func() tea.Msg {
				text, err := mcp.Call(mcpCfg, args)
				if err != nil {
					return mcpResultMsg{text: "suggest error: " + err.Error()}
				}
				feeds, err := parseSuggestJSON(text)
				if err != nil {
					return mcpResultMsg{text: "suggest parse error: " + err.Error() + "\n\n" + text}
				}
				return mcpSuggestMsg{feeds: feeds}
			}
		}
		question := fmt.Sprintf(`Summarize this article in %s for a senior engineer. Format: TL;DR (1 sentence), Key Points (bullet list of technical takeaways), Why It Matters (1-2 sentences on practical impact). No filler.`, mcpCfg.SummaryLanguage())
		args := map[string]any{
			"question": question,
			"context":  fmt.Sprintf("タイトル: %s\nURL: %s\n\n%s", a.Title, a.Link, a.Summary),
		}
		return fmt.Sprintf("running %s…", cmdName), func() tea.Msg {
			text, err := mcp.Call(mcpCfg, args)
			if err != nil {
				return mcpResultMsg{text: cmdName + " error: " + err.Error()}
			}
			return mcpResultMsg{text: text}
		}
	}

	// MCP not configured — fall back to the tailfeed API.
	if cmdName == "suggest" {
		article := a
		return "suggesting feeds…", func() tea.Msg {
			apiCfg, err := api.LoadOrRegister()
			if err != nil {
				return mcpResultMsg{text: "api: " + err.Error()}
			}
			feeds, err := api.Suggest(apiCfg.UserKey, article.Title, article.Link, article.Summary, "")
			if err != nil {
				return mcpResultMsg{text: "suggest error: " + err.Error()}
			}
			result := make([]suggestFeed, len(feeds))
			for i, f := range feeds {
				result[i] = suggestFeed{Title: f.Title, URL: f.URL, Description: f.Description}
			}
			return mcpSuggestMsg{feeds: result}
		}
	}
	article := a
	return fmt.Sprintf("running %s…", cmdName), func() tea.Msg {
		apiCfg, err := api.LoadOrRegister()
		if err != nil {
			return mcpResultMsg{text: "api: " + err.Error()}
		}
		text, err := api.Summary(apiCfg.UserKey, []api.SummaryArticle{
			{Title: article.Title, URL: article.Link, Summary: article.Summary},
		}, "Japanese")
		if err != nil {
			return mcpResultMsg{text: cmdName + " error: " + err.Error()}
		}
		return mcpResultMsg{text: text}
	}
}

// cmdUsage fetches /v1/usage and shows plan and remaining quota in the status bar.
func (m *Model) cmdUsage() (string, tea.Cmd) {
	apiCfg, err := api.LoadOrRegister()
	if err != nil {
		return "usage: " + err.Error(), nil
	}
	return "fetching usage…", func() tea.Msg {
		info, err := api.Usage(apiCfg.UserKey)
		if err != nil {
			return usageMsg{text: "usage: " + err.Error()}
		}
		text := fmt.Sprintf("plan: %s  ·  usage: %d/%d (summary, suggest)",
			info.Plan,
			info.SummaryRemaining, info.SummaryLimit,
		)
		if info.ResetAt != "" {
			text += "  ·  resets " + info.ResetAt
		}
		return usageMsg{text: text}
	}
}

// cmdMCPConfig handles "mcp set ...", "mcp list", "mcp on", "mcp off".
func (m *Model) cmdMCPConfig(args []string) string {
	if len(args) == 0 {
		return "usage: mcp set <command> [args...]  |  mcp list  |  mcp on  |  mcp off"
	}
	switch args[0] {
	case "set":
		if len(args) < 2 {
			return "usage: mcp set <command> [args...]"
		}
		cfg := &mcp.Config{Command: args[1], Args: args[2:]}
		if err := mcp.Save(cfg); err != nil {
			return "mcp: " + err.Error()
		}
		return fmt.Sprintf("mcp: registered → %s", args[1])
	case "list":
		cfg, err := mcp.LoadRaw()
		if err != nil {
			return "mcp: " + err.Error()
		}
		if cfg == nil {
			return "mcp: not configured"
		}
		status := "on"
		if cfg.Disabled {
			status = "off"
		}
		return fmt.Sprintf("mcp [%s]: %s %s", status, cfg.Command, strings.Join(cfg.Args, " "))
	case "on", "off":
		cfg, err := mcp.LoadRaw()
		if err != nil {
			return "mcp: " + err.Error()
		}
		if cfg == nil {
			return "mcp: not configured — use 'mcp set <command> [args...]' first"
		}
		cfg.Disabled = args[0] == "off"
		if err := mcp.Save(cfg); err != nil {
			return "mcp: " + err.Error()
		}
		return fmt.Sprintf("mcp: %s", args[0])
	}
	return "usage: mcp set <command> [args...]  |  mcp list  |  mcp on  |  mcp off"
}
