package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kumagaias/tailfeed/internal/db"
	"github.com/kumagaias/tailfeed/internal/mcp"
	"github.com/kumagaias/tailfeed/internal/summary"
)

// mcpResultMsg carries the result of an async MCP tools/call.
type mcpResultMsg struct{ text string }

// summaryHTMLMsg carries summary text and the generated HTML file path.
type summaryHTMLMsg struct {
	text string
	path string
}

// cmdSummaryToday summarises all articles published today through the configured MCP server.
func (m *Model) cmdSummaryToday() (string, tea.Cmd) {
	return m.cmdSummaryPeriod("today")
}

// cmdSummaryPeriod summarises articles for the given period through the configured MCP server.
func (m *Model) cmdSummaryPeriod(period string) (string, tea.Cmd) {
	var articles []db.Article
	var err error
	var label string
	switch period {
	case "yesterday":
		articles, err = m.db.ListYesterdayArticles()
		label = "yesterday"
	case "week":
		articles, err = m.db.ListWeekArticles()
		label = "last 7 days"
	default:
		articles, err = m.db.ListTodayArticles()
		label = "last 24 hours"
	}
	if err != nil {
		return "summary " + period + ": " + err.Error(), nil
	}
	if len(articles) == 0 {
		return "summary " + period + ": no articles for " + label, nil
	}
	originalCount := len(articles)
	articles = summary.LimitArticles(articles)
	summaryCfg, err := summary.LoadConfig()
	if err != nil {
		return "summary config: " + err.Error(), nil
	}
	mcpCfg, err := mcp.Load()
	if err != nil {
		return "mcp: " + err.Error(), nil
	}
	if mcpCfg == nil {
		return "mcp: not configured or disabled — use 'mcp set <command> [args...]'", nil
	}
	status := fmt.Sprintf("summarising %d articles (%s)…", len(articles), label)
	if originalCount > len(articles) {
		status = fmt.Sprintf("summarising %d of %d articles (%s; newest first)…", len(articles), originalCount, label)
	}
	return status, func() tea.Msg {
		language := summaryCfg.SummaryLanguage()
		if summaryCfg.Language == "" {
			language = mcpCfg.SummaryLanguage()
		}
		text, err := summary.SummarizeWithMCPInLanguage(mcpCfg, label, articles, summary.DefaultMaxContextRunes, summaryCfg.Theme, language)
		if err != nil {
			return mcpResultMsg{text: "summary " + period + " error: " + err.Error()}
		}
		path, htmlErr := writeSummaryHTML(text, articles)
		if htmlErr == nil {
			return summaryHTMLMsg{text: text, path: path}
		}
		return mcpResultMsg{text: text}
	}
}

// cmdMCP runs a summary or feed suggestion with the configured MCP server.
func (m *Model) cmdMCP(cmdName string, _ []string) (string, tea.Cmd) {
	if m.cursor >= len(m.articles) {
		return "no article selected", nil
	}
	a := m.articles[m.cursor]
	mcpCfg, err := mcp.Load()
	if err != nil {
		return "mcp: " + err.Error(), nil
	}
	if mcpCfg == nil {
		return "mcp: not configured or disabled — use 'mcp set <command> [args...]'", nil
	}
	if cmdName == "suggest" {
		args := map[string]any{
			"question": `You are a feed curator for engineers. Based on the article below, suggest 20 RSS feeds a senior developer would actually subscribe to — think official blogs, release notes, technical deep-dives, not generic news. Return ONLY valid JSON, no prose:
{"feeds":[{"title":"Feed Name","url":"https://...","description":"one-line description"},{"title":"Feed Name","url":"https://...","description":"one-line description"}]}`,
			"context": fmt.Sprintf("Title: %s\nURL: %s\n\n%s", a.Title, a.Link, a.Summary),
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
	summaryCfg, err := summary.LoadConfig()
	if err != nil {
		return "summary config: " + err.Error(), nil
	}
	themeLine := ""
	if theme := strings.TrimSpace(summaryCfg.Theme); theme != "" {
		themeLine = " Prioritize this user theme where relevant: " + theme + "."
	}
	language := summaryCfg.SummaryLanguage()
	if summaryCfg.Language == "" {
		language = mcpCfg.SummaryLanguage()
	}
	question := fmt.Sprintf(`Summarize this article in %s for a senior engineer.%s Keep the original article title unchanged. Use a compact plain bullet list in %s. Do not write labels like TL;DR, Summary, Key Points, or Why It Matters. No filler.`, language, themeLine, language)
	args := map[string]any{
		"question": question,
		"context":  fmt.Sprintf("Title: %s\nURL: %s\n\n%s", a.Title, a.Link, a.Summary),
	}
	return fmt.Sprintf("running %s…", cmdName), func() tea.Msg {
		text, err := mcp.Call(mcpCfg, args)
		if err != nil {
			return mcpResultMsg{text: cmdName + " error: " + err.Error()}
		}
		return mcpResultMsg{text: text}
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
