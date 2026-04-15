package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	gofeed "github.com/mmcdole/gofeed"

	"github.com/kumagaias/tailfeed/internal/db"
	"github.com/kumagaias/tailfeed/internal/feed"
	"github.com/kumagaias/tailfeed/internal/mcp"
)

// newArticleMsg is sent when the poller saves a new article.
type newArticleMsg db.Article

// errMsg wraps an error for display.
type errMsg struct{ err error }

// mcpResultMsg carries the result of an async MCP tools/call.
type mcpResultMsg struct{ text string }

// mcpSuggestMsg carries AI-suggested feeds.
type mcpSuggestMsg struct{ feeds []suggestFeed }

// suggestValidatedMsg carries feeds that passed RSS validation.
type suggestValidatedMsg struct{ feeds []suggestFeed }

// loadOlderMsg triggers a re-poll of all feeds to fetch older articles.
type loadOlderMsg struct{}

// loadOlderDoneMsg is sent after re-poll completes.
type loadOlderDoneMsg struct{ added int }

// listenForArticles converts the poller channel into a Bubble Tea command.
func listenForArticles(ch <-chan feed.NewArticleMsg) tea.Cmd {
	return func() tea.Msg {
		a := <-ch
		return newArticleMsg(a)
	}
}

// Update handles incoming messages.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport = viewport.New(m.listWidth(), m.contentHeight())
		m.detailVP = viewport.New(m.detailPaneWidth(), m.contentHeight())
		m.cursor = max(0, len(m.articles)-1)
		m.centerViewportOnCursor()
		m.updateDetailContent()
		return m, nil

	case loadOlderMsg:
		m.status = "loading older articles…"
		poller := m.poller
		return m, func() tea.Msg {
			before := 0
			poller.PollAll(context.Background())
			return loadOlderDoneMsg{added: before}
		}

	case loadOlderDoneMsg:
		prevLen := len(m.articles)
		_ = m.reloadArticles()
		added := len(m.articles) - prevLen
		if added > 0 {
			m.status = fmt.Sprintf("loaded %d older article(s)", added)
		} else {
			m.status = "no older articles found"
		}
		m.viewport.SetContent(m.renderArticles())
		return m, nil

	case newArticleMsg:
		atNewest := m.cursor == len(m.articles)-1
		_ = m.reloadArticles()
		if atNewest {
			m.cursor = max(0, len(m.articles)-1)
			m.centerViewportOnCursor()
		} else {
			m.viewport.SetContent(m.renderArticles())
		}
		return m, listenForArticles(m.poller.Articles())

	case errMsg:
		m.status = "error: " + msg.err.Error()
		return m, nil

	case summaryHTMLMsg:
		m.mcpResult = msg.text
		m.status = fmt.Sprintf("report ready — press o to open in browser")
		m.pendingHTMLPath = msg.path
		m.detailOpen = true
		m.resizeViewport()
		m.updateDetailContent()
		return m, nil

	case mcpResultMsg:
		m.mcpResult = msg.text
		m.status = ""
		m.detailOpen = true
		m.resizeViewport()
		m.updateDetailContent()
		return m, nil

	case mcpSuggestMsg:
		if len(msg.feeds) == 0 {
			m.status = "suggest: no feeds returned"
			return m, nil
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
		}

	case suggestValidatedMsg:
		if len(msg.feeds) == 0 {
			m.status = "suggest: no valid feeds found"
			return m, nil
		}
		m.status = ""
		m.suggestFeeds = msg.feeds
		m.suggestCursor = 0
		m.suggestSelected = make(map[int]bool)
		m.mode = modeSuggest
		return m, nil

	case suggestAddMsg:
		return m, func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			fp := gofeed.NewParser()
			if _, err := fp.ParseURLWithContext(msg.feed.URL, ctx); err != nil {
				return mcpResultMsg{text: fmt.Sprintf("suggest: %s is not a valid feed (%s), skipped", msg.feed.URL, err.Error())}
			}
			return suggestConfirmedMsg{feed: msg.feed}
		}

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
		}

	case suggestBatchConfirmedMsg:
		for _, url := range msg.added {
			m.cmdAdd([]string{url})
		}
		result := fmt.Sprintf("added %d feed(s)", len(msg.added))
		if len(msg.skipped) > 0 {
			result += fmt.Sprintf(", skipped %d invalid", len(msg.skipped))
		}
		m.status = result
		return m, nil

	case suggestConfirmedMsg:
		m.status = m.cmdAdd([]string{msg.feed.URL})
		return m, nil

	case tea.MouseMsg:
		if msg.Type == tea.MouseLeft && m.mode == modeNormal {
			// Header row right side (find box) — enter find mode.
			if msg.Y == 0 && msg.X >= m.width-26 {
				_, cmd := m.cmdFind(nil)
				return m, cmd
			}
			// Tab bar is at row 1 — switch group or activate search.
			if msg.Y == 1 {
				searchWidth := lipgloss.Width(styleInput.Render("🔍 search..."))
				if msg.X >= m.width-searchWidth {
					_, cmd := m.cmdFind(nil)
					return m, cmd
				}
				for i, r := range m.tabXRanges() {
					if msg.X >= r[0] && msg.X < r[1] && i != m.tabIdx {
						prev := m.tabIdx
						m.tabIdx = i
						m.reloadGroupPreservePos(prev)
						return m, nil
					}
				}
			}
			// Article area starts at row 3 (header + tabbar + separator).
			const articleAreaTop = 3
			if msg.Y >= articleAreaTop {
				contentY := msg.Y - articleAreaTop + m.viewport.YOffset
				idx := contentY / linesPerSlot
				lineInSlot := contentY % linesPerSlot
				if idx >= 0 && idx < len(m.articles) {
					// Click on the heart position (title line, col 3) → toggle stock.
					if lineInSlot == 1 && msg.X == 3 {
						_ = m.db.ToggleStock(m.articles[idx].ID)
						_ = m.reloadArticles()
						m.viewport.SetContent(m.renderArticles())
						m.updateDetailContent()
					} else if link := m.articles[idx].Link; link != "" {
						_ = openBrowser(link)
					}
				}
				return m, nil
			}
		}
		// Delegate scroll events to viewport.
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		switch m.mode {
		case modeCommand:
			return m.updateCommand(msg)
		case modeFeedList:
			return m.updateFeedList(msg)
		case modeSuggest:
			return m.updateSuggest(msg)
		case modeFind:
			return m.updateFind(msg)
		case modeSuggestInput:
			return m.updateSuggestInput(msg)
		default:
			return m.updateNormal(msg)
		}
	}
	return m, nil
}

func (m *Model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// gg detection: two consecutive "g" presses
	if key.Matches(msg, keys.GotoTopG) {
		if m.pendingG {
			m.pendingG = false
			m.jumpToOldest()
			m.updateDetailContent()
			return m, nil
		}
		m.pendingG = true
		return m, nil
	}
	m.pendingG = false // any other key cancels pending g

	switch {
	case msg.String() == "y":
		if m.pendingGroupDel != "" {
			name := m.pendingGroupDel
			m.pendingGroupDel = ""
			m.status = m.execGroupDel(name)
			return m, nil
		}

	case msg.String() == "esc":
		if m.detailOpen {
			m.detailOpen = false
			m.resizeViewport()
			return m, nil
		}
		if m.pendingGroupDel != "" {
			m.pendingGroupDel = ""
			m.status = ""
			return m, nil
		}
		if m.status != "" {
			m.status = ""
			return m, nil
		}

	case key.Matches(msg, keys.Quit), msg.String() == "ctrl+c":
		return m, tea.Quit

	case key.Matches(msg, keys.GotoBottom):
		m.jumpToNewest()
		m.updateDetailContent()

	case key.Matches(msg, keys.PageDown):
		m.viewport.HalfViewDown()

	case key.Matches(msg, keys.PageUp):
		m.viewport.HalfViewUp()

	case key.Matches(msg, keys.Command):
		m.mode = modeCommand
		m.input.SetValue("")
		m.input.Focus()
		if m.tabIdx > 0 && m.tabIdx < len(m.tabs) && !m.tabs[m.tabIdx].isStock {
			m.input.Placeholder = commandPlaceholder(false)
		} else {
			m.input.Placeholder = commandPlaceholder(true)
		}
		m.resizeViewport()
		return m, textinput.Blink

	case key.Matches(msg, keys.Left):
		if m.detailOpen {
			m.detailOpen = false
			m.resizeViewport()
		} else if m.cursor > 0 {
			m.cursor--
			m.syncViewportToCursor()
		}

	case key.Matches(msg, keys.Right):
		if !m.detailOpen {
			m.detailOpen = true
			m.resizeViewport()
			m.updateDetailContent()
		}

	case key.Matches(msg, keys.PrevGroup):
		if m.tabIdx > 0 {
			prev := m.tabIdx
			m.tabIdx--
			m.reloadGroupPreservePos(prev)
		}

	case key.Matches(msg, keys.NextGroup):
		if m.tabIdx < len(m.tabs)-1 {
			prev := m.tabIdx
			m.tabIdx++
			m.reloadGroupPreservePos(prev)
		}

	case key.Matches(msg, keys.Up):
		if m.cursor > 0 {
			m.cursor--
			m.mcpResult = ""
			m.syncViewportToCursor()
			m.updateDetailContent()
		} else if m.articlesOffset > 0 {
			// Already at oldest visible — do nothing (offset already loaded).
		} else {
			// At the very top: try to load older articles.
			return m, func() tea.Msg { return loadOlderMsg{} }
		}

	case key.Matches(msg, keys.Down):
		if m.cursor < len(m.articles)-1 {
			m.cursor++
			m.mcpResult = ""
			m.syncViewportToCursor()
			m.updateDetailContent()
		}

	case key.Matches(msg, keys.ViewDetail):
		m.detailOpen = !m.detailOpen
		m.resizeViewport()
		m.updateDetailContent()

	case key.Matches(msg, keys.Open):
		if m.pendingHTMLPath != "" {
			_ = openBrowser("file://" + m.pendingHTMLPath)
			m.pendingHTMLPath = ""
			m.status = ""
			return m, nil
		}
		if m.cursor < len(m.articles) {
			if link := m.articles[m.cursor].Link; link != "" {
				_ = openBrowser(link)
			}
		}

	case key.Matches(msg, keys.MarkRead):
		if m.cursor < len(m.articles) {
			_ = m.db.MarkRead(m.articles[m.cursor].ID)
			_ = m.reloadArticles()
			m.viewport.SetContent(m.renderArticles())
		}

	case key.Matches(msg, keys.Stock):
		if m.cursor < len(m.articles) {
			_ = m.db.ToggleStock(m.articles[m.cursor].ID)
			_ = m.reloadArticles()
			m.viewport.SetContent(m.renderArticles())
			m.updateDetailContent()
		}
	}
	return m, nil
}

func (m *Model) updateCommand(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Cancel):
		m.mode = modeNormal
		m.input.Blur()
		m.status = ""
		m.resizeViewport()
		return m, nil

	case key.Matches(msg, keys.Confirm):
		cmd := strings.TrimSpace(m.input.Value())
		m.mode = modeNormal
		m.input.Blur()
		status, teaCmd := m.execCommand(cmd)
		m.status = status
		m.resizeViewport()
		return m, teaCmd
	}

	var tiCmd tea.Cmd
	m.input, tiCmd = m.input.Update(msg)
	return m, tiCmd
}

// reloadGroupPreservePos switches to m.tabIdx's articles.
// prevTab is the tab index we are leaving; its cursor position is saved.
// If the destination tab has a saved cursor it is restored, otherwise cursor
// stays at 0.
func (m *Model) reloadGroupPreservePos(prevTab int) {
	// Save cursor and scroll position for the tab we are leaving.
	m.tabCursors[prevTab] = m.cursor
	m.tabOffsets[prevTab] = m.viewport.YOffset
	m.articlesOffset = 0

	_ = m.reloadArticles() // loads articles for m.tabIdx, clamps cursor

	// Restore saved cursor and scroll position for the new tab.
	if saved, ok := m.tabCursors[m.tabIdx]; ok {
		if saved < len(m.articles) {
			m.cursor = saved
		}
	}
	if offset, ok := m.tabOffsets[m.tabIdx]; ok {
		m.viewport.Width = m.listWidth()
		m.viewport.Height = m.contentHeight()
		m.viewport.SetContent(m.renderArticles())
		m.viewport.SetYOffset(offset)
	} else {
		m.centerViewportOnCursor()
	}
	m.updateDetailContent()
}

// scrolloff is the minimum number of lines kept above/below the cursor card.
const scrolloff = 2

// syncViewportToCursor re-renders and scrolls just enough to keep the cursor
// card visible with scrolloff padding (vim-style). Only G/gg force centering.
func (m *Model) syncViewportToCursor() {
	// Always refresh dimensions before calculating scroll position.
	m.viewport.Width = m.listWidth()
	m.viewport.Height = m.contentHeight()
	m.viewport.SetContent(m.renderArticles())

	cardTop := m.cursor * linesPerSlot
	cardBot := cardTop + linesPerCard // exclusive bottom line of card

	top := m.viewport.YOffset
	bot := top + m.viewport.Height

	// scrolloff below: don't exceed total content height
	totalLines := len(m.articles) * linesPerSlot
	bottomScrolloff := scrolloff
	if cardBot+bottomScrolloff > totalLines {
		bottomScrolloff = totalLines - cardBot
	}

	if cardTop-scrolloff < top {
		m.viewport.SetYOffset(max(0, cardTop-scrolloff))
	} else if cardBot+bottomScrolloff > bot {
		m.viewport.SetYOffset(cardBot + bottomScrolloff - m.viewport.Height)
	}
}

// centerViewportOnCursor scrolls so the cursor card is exactly in the middle.
// Used by G and gg.
func (m *Model) centerViewportOnCursor() {
	m.viewport.Width = m.listWidth()
	m.viewport.Height = m.contentHeight()
	m.viewport.SetContent(m.renderArticles())
	cardTop := m.cursor * linesPerSlot
	center := cardTop + linesPerCard/2
	offset := center - m.viewport.Height/2
	if offset < 0 {
		offset = 0
	}
	// Ensure the card bottom is always visible.
	cardBot := cardTop + linesPerCard
	if cardBot > offset+m.viewport.Height {
		offset = cardBot - m.viewport.Height
	}
	m.viewport.SetYOffset(offset)
}

func (m *Model) contentHeight() int {
	// Rows consumed outside the viewport:
	//   header\n + tabbar\n + separator\n  = 3 rows above
	//   \n (before footer) + footer        = 2 rows below  → total chrome = 5
	// Command mode: footer = inputBoxHeight rows instead of 1
	if m.mode == modeCommand || m.mode == modeSuggestInput {
		return m.height - 4 - inputBoxHeight
	}
	return m.height - 5
}

func (m *Model) resizeViewport() {
	h := m.contentHeight()
	m.viewport.Width = m.listWidth()
	m.viewport.Height = h
	m.viewport.SetContent(m.renderArticles())
	m.detailVP.Width = m.detailPaneWidth()
	m.detailVP.Height = h
	m.updateDetailContent()
}

// updateDetailContent re-renders the detail pane for the current cursor position.
func (m *Model) updateDetailContent() {
	if !m.detailOpen || m.detailPaneWidth() <= 0 {
		return
	}
	m.detailVP.SetYOffset(0)
	m.detailVP.SetContent(m.renderDetailContent())
}

// ── Command palette ───────────────────────────────────────────────────────────

func (m *Model) execCommand(raw string) (string, tea.Cmd) {
	parts := strings.Fields(raw)
	if len(parts) == 0 {
		return "", nil
	}
	switch parts[0] {
	case "add":
		return m.cmdAdd(parts[1:]), nil
	case "remove", "rm":
		return m.cmdRemove(parts[1:]), nil
	case "group":
		if len(parts) < 2 {
			return "usage: group new <name> | group del <name>", nil
		}
		switch parts[1] {
		case "new":
			return m.cmdGroupNew(parts[2:]), nil
		case "del", "delete":
			return m.cmdGroupDel(parts[2:]), nil
		}
	case "list", "ls":
		return m.cmdList(), nil
	case "help", "?":
		return m.cmdHelp(), nil
	case "find", "/":
		return m.cmdFind(parts[1:])
	case "suggest":
		if len(parts) > 1 {
			return m.cmdMCP("suggest", parts[1:])
		}
		return m.cmdSuggestInput()
	case "summary":
		if len(parts) > 1 && parts[1] == "today" {
			return m.cmdSummaryToday()
		}
		return m.cmdMCP("summary", parts[1:])
	case "mcp":
		return m.cmdMCPConfig(parts[1:]), nil
	}
	return fmt.Sprintf("unknown command: %s — type help for usage", parts[0]), nil
}

func (m *Model) cmdAdd(args []string) string {
	if len(args) == 0 {
		return "usage: add <url> [--group <name>]"
	}
	url := args[0]
	groupID := m.currentGroupID()

	for i, a := range args[1:] {
		if a == "--group" && i+2 < len(args) {
			g, err := m.db.GetGroupByName(args[i+2])
			if err != nil {
				return fmt.Sprintf("group %q not found", args[i+2])
			}
			groupID = &g.ID
		}
	}

	f, err := m.db.AddFeed(url, groupID)
	if err != nil {
		return "error: " + err.Error()
	}
	go m.poller.PollFeed(context.Background(), *f)
	_ = m.reloadArticles()
	return fmt.Sprintf("added %s", url)
}

func (m *Model) cmdRemove(args []string) string {
	if len(args) == 0 {
		return "usage: remove <url>"
	}
	if err := m.db.RemoveFeed(args[0]); err != nil {
		return "error: " + err.Error()
	}
	_ = m.reloadArticles()
	return fmt.Sprintf("removed %s", args[0])
}

func (m *Model) cmdGroupNew(args []string) string {
	if len(args) == 0 {
		return "usage: group new <name>"
	}
	name := strings.Join(args, " ")
	if _, err := m.db.CreateGroup(name); err != nil {
		return "error: " + err.Error()
	}
	_ = m.reloadTabs()
	return fmt.Sprintf("created group %q", name)
}

func (m *Model) cmdList() string {
	m.reloadFeedList()
	m.feedListCursor = 0
	m.mode = modeFeedList
	return ""
}

func (m *Model) cmdHelp() string {
	m.feedListItems = []string{
		"Navigation",
		"  ↑↓ / j k        move cursor",
		"  ← → / h l       prev/next article + open detail",
		"  G                newest article",
		"  gg               oldest article",
		"  ^F ^J / ^B ^K   page down / up",
		"",
		"Detail pane",
		"  →  / l          open detail",
		"  ←  / h          close detail",
		"  v                toggle detail",
		"  esc              close detail",
		"",
		"Groups",
		"  [ ^ H Shift+←   previous group",
		"  ] $ L Shift+→   next group",
		"  mouse click      switch group",
		"",
		"Articles",
		"  enter / o        open in browser",
		"  mouse click      open in browser",
		"  space            toggle ♥ stock",
		"  click ♥          toggle stock",
		"  m                mark read",
		"",
		"Commands  (press /)",
		"  add <url>             subscribe to feed",
		"  remove <url>          unsubscribe",
		"  find <keyword>        filter articles by keyword (esc to clear)",
		"  list                  manage feeds",
		"  group new <name>      create group",
		"  group del [name]      delete group (omit name = current group)",
		"  suggest               suggest related feeds (mcp required)",
		"  summary               summarize current article (mcp required)",
		"  summary today         summarize all articles from today (mcp required)",
		"  mcp set <name> <cmd> [args...]  register MCP server",
		"  mcp list              list configured MCP servers",
		"  help                  show this help",
		"",
		"  q / ctrl+c    quit",
	}
	m.feedListFeeds = nil
	m.feedListCursor = 0
	m.mode = modeFeedList
	return ""
}

// cmdSummaryToday summarises all articles published today via MCP.
func (m *Model) cmdSummaryToday() (string, tea.Cmd) {
	cfg, err := mcp.Load()
	if err != nil {
		return "mcp: " + err.Error(), nil
	}
	if cfg == nil {
		return "mcp: not configured — use: tailfeed mcp set <command> [args...]", nil
	}
	articles, err := m.db.ListTodayArticles()
	if err != nil {
		return "summary today: " + err.Error(), nil
	}
	if len(articles) == 0 {
		return "summary today: no articles today", nil
	}
	var sb strings.Builder
	for _, a := range articles {
		sb.WriteString(fmt.Sprintf("## %s\nURL: %s\n%s\n\n", a.Title, a.Link, a.Summary))
	}
	context := sb.String()
	args := map[string]any{
		"question": fmt.Sprintf(`You are a senior engineer's daily briefing assistant. Summarize today's %d articles in %s for a technical audience. For each article: one-line TL;DR, key technical points as bullet list. End with a "## Today's Signal" section: 2-3 sentences on trends worth watching. Be concise, skip fluff.`, len(articles), cfg.SummaryLanguage()),
		"context":  context,
	}
	return fmt.Sprintf("summarising %d articles…", len(articles)), func() tea.Msg {
		text, err := mcp.Call(cfg, args)
		if err != nil {
			return mcpResultMsg{text: "summary today error: " + err.Error()}
		}
		// Generate HTML report.
		path, htmlErr := writeSummaryHTML(text, articles)
		if htmlErr == nil {
			return summaryHTMLMsg{text: text, path: path}
		}
		return mcpResultMsg{text: text}
	}
}

// summaryHTMLMsg carries summary text and the generated HTML file path.
type summaryHTMLMsg struct {
	text string
	path string
}

// cmdMCP runs toolName against the configured MCP server.
func (m *Model) cmdMCP(cmdName string, _ []string) (string, tea.Cmd) {
	cfg, err := mcp.Load()
	if err != nil {
		return "mcp: " + err.Error(), nil
	}
	if cfg == nil {
		return "mcp: not configured — use: tailfeed mcp set <command> [args...]", nil
	}
	if m.cursor >= len(m.articles) {
		return "mcp: no article selected", nil
	}
	a := m.articles[m.cursor]
	if cmdName == "suggest" {
		args := map[string]any{
			"question": `You are a feed curator for engineers. Based on the article below, suggest 20 RSS feeds a senior developer would actually subscribe to — think official blogs, release notes, technical deep-dives, not generic news. Return ONLY valid JSON, no prose:
{"feeds":[{"title":"Feed Name","url":"https://...","description":"one-line description"},{"title":"Feed Name","url":"https://...","description":"one-line description"}]}`,
			"context": fmt.Sprintf("タイトル: %s\nURL: %s\n\n%s", a.Title, a.Link, a.Summary),
		}
		return "suggesting feeds…", func() tea.Msg {
			text, err := mcp.Call(cfg, args)
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
	question := fmt.Sprintf(`Summarize this article in %s for a senior engineer. Format: TL;DR (1 sentence), Key Points (bullet list of technical takeaways), Why It Matters (1-2 sentences on practical impact). No filler.`, cfg.SummaryLanguage())
	args := map[string]any{
		"question": question,
		"context":  fmt.Sprintf("タイトル: %s\nURL: %s\n\n%s", a.Title, a.Link, a.Summary),
	}
	return fmt.Sprintf("running %s…", cmdName), func() tea.Msg {
		text, err := mcp.Call(cfg, args)
		if err != nil {
			return mcpResultMsg{text: cmdName + " error: " + err.Error()}
		}
		return mcpResultMsg{text: text}
	}
}

// cmdMCPConfig handles "mcp set ..." and "mcp list".
func (m *Model) cmdMCPConfig(args []string) string {
	if len(args) == 0 {
		return "usage: mcp set <command> [args...]  |  mcp list"
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
		cfg, err := mcp.Load()
		if err != nil {
			return "mcp: " + err.Error()
		}
		if cfg == nil {
			return "mcp: not configured"
		}
		return fmt.Sprintf("mcp: %s %s", cfg.Command, strings.Join(cfg.Args, " "))
	}
	return "usage: mcp set <command> [args...]  |  mcp list"
}

// reloadFeedList refreshes feedListItems and feedListFeeds without touching cursor or mode.
// cmdFind enters find mode with an optional initial query.
func (m *Model) cmdFind(args []string) (string, tea.Cmd) {
	m.mode = modeFind
	m.input.Placeholder = "search…"
	m.input.SetValue(strings.Join(args, " "))
	m.input.Focus()
	m.input.CursorEnd()
	m.filterQuery = m.input.Value()
	m.cursor = 0
	_ = m.reloadArticles()
	m.syncViewportToCursor()
	return "", nil
}

// updateFind handles key events in modeFind.
func (m *Model) updateFind(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Cancel) || msg.String() == "esc":
		m.mode = modeNormal
		m.input.Blur()
		m.filterQuery = ""
		m.cursor = 0
		_ = m.reloadArticles()
		m.syncViewportToCursor()
		m.resizeViewport()
		return m, nil
	case key.Matches(msg, keys.Confirm):
		m.mode = modeNormal
		m.input.Blur()
		m.resizeViewport()
		return m, nil
	}
	var tiCmd tea.Cmd
	m.input, tiCmd = m.input.Update(msg)
	m.filterQuery = m.input.Value()
	m.cursor = 0
	_ = m.reloadArticles()
	m.syncViewportToCursor()
	return m, tiCmd
}

// parseSuggestJSON extracts feeds from AI response
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

// cmdMCPSuggestFreeText runs suggest with a free-text theme query (no article context).
func (m *Model) cmdMCPSuggestFreeText(query string) (string, tea.Cmd) {
	cfg, err := mcp.Load()
	if err != nil {
		return "mcp: " + err.Error(), nil
	}
	if cfg == nil {
		return "mcp: not configured — use: mcp set <command> [args...]", nil
	}
	args := map[string]any{
		"question": fmt.Sprintf(`You are a feed curator for engineers. Suggest 20 RSS feeds about "%s" that a senior developer would actually subscribe to — think official blogs, release notes, changelogs, technical deep-dives. Return ONLY valid JSON, no prose:
{"feeds":[{"title":"Feed Name","url":"https://...","description":"one-line description"},{"title":"Feed Name","url":"https://...","description":"one-line description"}]}`, query),
		"context": "",
	}
	return "suggesting feeds…", func() tea.Msg {
		text, err := mcp.Call(cfg, args)
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
		// Toggle selection of current item.
		if m.suggestSelected == nil {
			m.suggestSelected = make(map[int]bool)
		}
		m.suggestSelected[m.suggestCursor] = !m.suggestSelected[m.suggestCursor]
	case key.Matches(msg, keys.Confirm):
		// If nothing selected, add only the cursor item.
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
	// Confirmation state: only y/enter or esc are meaningful.
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

func (m *Model) cmdGroupDel(args []string) string {
	var name string
	if len(args) > 0 {
		name = strings.Join(args, " ")
	} else {
		// No argument: use the currently viewed group.
		if m.tabIdx == 0 || m.tabIdx >= len(m.tabs) || m.tabs[m.tabIdx].isStock {
			return "cannot delete All or Stock"
		}
		name = m.tabs[m.tabIdx].name
	}

	// Guard virtual tabs even when named explicitly.
	if name == "All" || name == "♥ Stock" {
		return fmt.Sprintf("cannot delete %q", name)
	}

	if _, err := m.db.GetGroupByName(name); err != nil {
		return fmt.Sprintf("group %q not found", name)
	}

	m.pendingGroupDel = name
	return fmt.Sprintf("delete group %q? press y to confirm, esc to cancel", name)
}

func (m *Model) execGroupDel(name string) string {
	g, err := m.db.GetGroupByName(name)
	if err != nil {
		return fmt.Sprintf("group %q not found", name)
	}
	if err := m.db.DeleteGroup(g.ID); err != nil {
		return "error: " + err.Error()
	}
	_ = m.reloadTabs()
	_ = m.reloadArticles()
	m.viewport.SetContent(m.renderArticles())
	return fmt.Sprintf("deleted group %q", name)
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
