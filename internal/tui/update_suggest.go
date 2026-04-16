package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	gofeed "github.com/mmcdole/gofeed"

	"github.com/kumagaias/tailfeed/internal/api"
	"github.com/kumagaias/tailfeed/internal/mcp"
)

// suggestAddMsg triggers URL check + add for a selected suggested feed.
type suggestAddMsg struct{ feed suggestFeed }

// suggestBatchMsg triggers sequential URL check + add for multiple feeds.
type suggestBatchMsg struct{ feeds []suggestFeed }

// suggestBatchConfirmedMsg carries results of batch feed validation.
type suggestBatchConfirmedMsg struct {
	added   []string
	skipped []string
}

// suggestConfirmedMsg is sent after URL existence check passes.
type suggestConfirmedMsg struct{ feed suggestFeed }

// mcpSuggestMsg carries AI-suggested feeds.
type mcpSuggestMsg struct{ feeds []suggestFeed }

// suggestValidatedMsg carries feeds that passed RSS validation.
type suggestValidatedMsg struct{ feeds []suggestFeed }

// cmdSuggestInput enters the free-text suggest input mode.
func (m *Model) cmdSuggestInput() (string, tea.Cmd) {
	m.mode = modeSuggestInput
	m.input.Placeholder = "テーマや欲しいRSSの種類を入力… (enter で検索, esc でキャンセル)"
	m.input.SetValue("")
	m.input.Focus()
	m.resizeViewport()
	return "", textinput.Blink
}

// updateSuggestInput handles key events in modeSuggestInput.
func (m *Model) updateSuggestInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Cancel):
		m.mode = modeNormal
		m.input.Blur()
		m.resizeViewport()
		return m, nil
	case key.Matches(msg, keys.Confirm):
		query := strings.TrimSpace(m.input.Value())
		m.mode = modeNormal
		m.input.Blur()
		m.resizeViewport()
		if query == "" {
			return m, nil
		}
		status, cmd := m.cmdMCPSuggestFreeText(query)
		m.status = status
		return m, cmd
	}
	var tiCmd tea.Cmd
	m.input, tiCmd = m.input.Update(msg)
	return m, tiCmd
}

// cmdMCPSuggestFreeText runs suggest with a free-text theme query (no article context),
// falling back to the tailfeed API when no MCP server is configured.
func (m *Model) cmdMCPSuggestFreeText(query string) (string, tea.Cmd) {
	mcpCfg, err := mcp.Load()
	if err != nil {
		return "mcp: " + err.Error(), nil
	}
	if mcpCfg != nil {
		args := map[string]any{
			"question": fmt.Sprintf(`You are a feed curator for engineers. Suggest 20 RSS feeds about "%s" that a senior developer would actually subscribe to — think official blogs, release notes, changelogs, technical deep-dives. Return ONLY valid JSON, no prose:
{"feeds":[{"title":"Feed Name","url":"https://...","description":"one-line description"},{"title":"Feed Name","url":"https://...","description":"one-line description"}]}`, query),
			"context": "",
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

	return "suggesting feeds…", func() tea.Msg {
		apiCfg, err := api.LoadOrRegister()
		if err != nil {
			return mcpResultMsg{text: "api: " + err.Error()}
		}
		feeds, err := api.Suggest(apiCfg.UserKey, "", "", "", query)
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

// updateSuggest handles key events in modeSuggest.
func (m *Model) updateSuggest(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Cancel):
		m.mode = modeNormal
		m.suggestFeeds = nil
		m.suggestSelected = nil
	case key.Matches(msg, keys.Up):
		if m.suggestCursor > 0 {
			m.suggestCursor--
		}
	case key.Matches(msg, keys.Down):
		if m.suggestCursor < len(m.suggestFeeds)-1 {
			m.suggestCursor++
		}
	case msg.String() == " ":
		if m.suggestSelected == nil {
			m.suggestSelected = make(map[int]bool)
		}
		m.suggestSelected[m.suggestCursor] = !m.suggestSelected[m.suggestCursor]
	case key.Matches(msg, keys.Confirm):
		targets := []suggestFeed{}
		for i, f := range m.suggestFeeds {
			if m.suggestSelected[i] {
				targets = append(targets, f)
			}
		}
		if len(targets) == 0 && m.suggestCursor < len(m.suggestFeeds) {
			targets = []suggestFeed{m.suggestFeeds[m.suggestCursor]}
		}
		m.mode = modeNormal
		m.suggestFeeds = nil
		m.suggestSelected = nil
		if len(targets) == 0 {
			return m, nil
		}
		return m, func() tea.Msg { return suggestBatchMsg{feeds: targets} }
	}
	return m, nil
}

// reloadFeedList refreshes feedListItems and feedListFeeds.
func (m *Model) reloadFeedList() {
	feeds, err := m.db.ListFeeds(nil)
	if err != nil {
		return
	}
	groups, _ := m.db.ListGroups()
	groupName := map[int64]string{}
	for _, g := range groups {
		groupName[g.ID] = g.Name
	}

	items := make([]string, 0, len(feeds))
	for _, f := range feeds {
		label := f.Title
		if label == "" {
			label = f.URL
		}
		group := "(ungrouped)"
		if f.GroupID != nil {
			if n, ok := groupName[*f.GroupID]; ok {
				group = n
			}
		}
		items = append(items, fmt.Sprintf("[%s] %s", group, label))
	}

	m.feedListItems = items
	m.feedListFeeds = feeds
}

// updateFeedList handles key events when the feed list overlay is open.
func (m *Model) updateFeedList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.feedListConfirm {
		switch msg.String() {
		case "y", "enter":
			m.feedListConfirm = false
			if m.feedListCursor < len(m.feedListFeeds) {
				f := m.feedListFeeds[m.feedListCursor]
				if err := m.db.RemoveFeed(f.URL); err != nil {
					m.status = "error: " + err.Error()
				} else {
					m.reloadFeedList()
					if m.feedListCursor >= len(m.feedListItems) {
						m.feedListCursor = max(0, len(m.feedListItems)-1)
					}
					_ = m.reloadArticles()
					m.viewport.SetContent(m.renderArticles())
				}
			}
		default:
			m.feedListConfirm = false
		}
		return m, nil
	}

	switch {
	case key.Matches(msg, keys.Cancel) || key.Matches(msg, keys.Quit):
		m.mode = modeNormal
		m.feedListItems = nil
		m.feedListFeeds = nil
		m.feedListCursor = 0
	case key.Matches(msg, keys.Up):
		if m.feedListCursor > 0 {
			m.feedListCursor--
		}
	case key.Matches(msg, keys.Down):
		if m.feedListCursor < len(m.feedListItems)-1 {
			m.feedListCursor++
		}
	case key.Matches(msg, keys.Delete):
		if len(m.feedListFeeds) > 0 {
			m.feedListConfirm = true
		}
	}
	return m, nil
}

// handleSuggestMsgs processes suggest-related messages in Update.
func (m *Model) handleSuggestMsgs(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case mcpSuggestMsg:
		if len(msg.feeds) == 0 {
			m.status = "suggest: no feeds returned"
			return m, nil, true
		}
		m.status = fmt.Sprintf("validating %d feeds…", len(msg.feeds))
		feeds := msg.feeds
		return m, func() tea.Msg {
			valid := make([]suggestFeed, 0, len(feeds))
			for _, f := range feeds {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_, err := gofeed.NewParser().ParseURLWithContext(f.URL, ctx)
				cancel()
				if err == nil {
					valid = append(valid, f)
				}
			}
			return suggestValidatedMsg{feeds: valid}
		}, true

	case suggestValidatedMsg:
		if len(msg.feeds) == 0 {
			m.status = "suggest: no valid feeds found"
			return m, nil, true
		}
		m.status = ""
		m.suggestFeeds = msg.feeds
		m.suggestCursor = 0
		m.suggestSelected = make(map[int]bool)
		m.mode = modeSuggest
		return m, nil, true

	case suggestAddMsg:
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			fp := gofeed.NewParser()
			if _, err := fp.ParseURLWithContext(msg.feed.URL, ctx); err != nil {
				return mcpResultMsg{text: fmt.Sprintf("suggest: %s is not a valid feed (%s), skipped", msg.feed.URL, err.Error())}
			}
			return suggestConfirmedMsg{feed: msg.feed}
		}, true

	case suggestBatchMsg:
		return m, func() tea.Msg {
			added, skipped := []string{}, []string{}
			for _, f := range msg.feeds {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				_, err := gofeed.NewParser().ParseURLWithContext(f.URL, ctx)
				cancel()
				if err != nil {
					skipped = append(skipped, f.Title)
					continue
				}
				added = append(added, f.URL)
			}
			return suggestBatchConfirmedMsg{added: added, skipped: skipped}
		}, true

	case suggestBatchConfirmedMsg:
		for _, url := range msg.added {
			m.cmdAdd([]string{url})
		}
		result := fmt.Sprintf("added %d feed(s)", len(msg.added))
		if len(msg.skipped) > 0 {
			result += fmt.Sprintf(", skipped %d invalid", len(msg.skipped))
		}
		m.status = result
		return m, nil, true

	case suggestConfirmedMsg:
		m.status = m.cmdAdd([]string{msg.feed.URL})
		return m, nil, true
	}
	return m, nil, false
}
