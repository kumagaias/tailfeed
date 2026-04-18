package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kumagaias/tailfeed/internal/db"
	"github.com/kumagaias/tailfeed/internal/feed"
)

// newArticleMsg is sent when the poller saves a new article.
type newArticleMsg db.Article

// errMsg wraps an error for display.
type errMsg struct{ err error }

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
	// Suggest-related messages.
	if model, cmd, handled := m.handleSuggestMsgs(msg); handled {
		return model, cmd
	}

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
			poller.PollAll(context.Background())
			return loadOlderDoneMsg{}
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
		m.status = "report ready — press o to open in browser"
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

	case usageMsg:
		m.status = msg.text
		return m, nil

	case tea.MouseMsg:
		return m.handleMouseMsg(msg)

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

func (m *Model) handleMouseMsg(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.MouseLeft && m.mode == modeNormal {
		if msg.Y == 0 && msg.X >= m.width-26 {
			_, cmd := m.cmdFind(nil)
			return m, cmd
		}
		if msg.Y == 1 {
			searchWidth := lipgloss.Width("[" + styleHelp.Render("🔍 search...") + "]")
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
		const articleAreaTop = 3
		if msg.Y >= articleAreaTop {
			contentY := msg.Y - articleAreaTop + m.viewport.YOffset
			idx := contentY / linesPerSlot
			lineInSlot := contentY % linesPerSlot
			if idx >= 0 && idx < len(m.articles) {
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
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *Model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	m.pendingG = false

	switch {
	case msg.String() == "y":
		return m.handleYKey()
	case msg.String() == "esc":
		return m.handleEscKey()
	case key.Matches(msg, keys.Quit), msg.String() == "ctrl+c":
		return m, tea.Quit
	case key.Matches(msg, keys.GotoBottom):
		m.jumpToNewest()
		m.updateDetailContent()
	case key.Matches(msg, keys.PageDown):
		m.cursor = min(len(m.articles)-1, m.cursor+m.viewport.Height)
		m.centerViewportOnCursor()
		m.updateDetailContent()
	case key.Matches(msg, keys.PageUp):
		m.cursor = max(0, m.cursor-m.viewport.Height)
		m.centerViewportOnCursor()
		m.updateDetailContent()
	case key.Matches(msg, keys.HalfDown):
		m.cursor = min(len(m.articles)-1, m.cursor+m.viewport.Height/2)
		m.centerViewportOnCursor()
		m.updateDetailContent()
	case key.Matches(msg, keys.HalfUp):
		m.cursor = max(0, m.cursor-m.viewport.Height/2)
		m.centerViewportOnCursor()
		m.updateDetailContent()
	case key.Matches(msg, keys.Command):
		return m.enterCommandMode()
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
		} else if m.articlesOffset <= 0 {
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
		return m.handleOpenKey()
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

func (m *Model) handleYKey() (tea.Model, tea.Cmd) {
	if m.pendingClear {
		m.pendingClear = false
		if err := m.db.Clear(); err != nil {
			m.status = "clear: " + err.Error()
		} else {
			m.status = "cleared: all feeds, articles, and groups removed"
			_ = m.reloadTabs()
			_ = m.reloadArticles()
			m.cursor = 0
			m.viewport.SetContent(m.renderArticles())
		}
		return m, nil
	}
	if m.pendingGroupDel != "" {
		name := m.pendingGroupDel
		m.pendingGroupDel = ""
		m.status = m.execGroupDel(name)
	}
	return m, nil
}

func (m *Model) handleEscKey() (tea.Model, tea.Cmd) {
	switch {
	case m.pendingClear:
		m.pendingClear = false
		m.status = ""
		m.resizeViewport()
	case m.detailOpen:
		m.detailOpen = false
		m.resizeViewport()
	case m.pendingGroupDel != "":
		m.pendingGroupDel = ""
		m.status = ""
		m.resizeViewport()
	case m.status != "":
		m.status = ""
		m.resizeViewport()
	}
	return m, nil
}

func (m *Model) handleOpenKey() (tea.Model, tea.Cmd) {
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
	return m, nil
}

func (m *Model) enterCommandMode() (tea.Model, tea.Cmd) {
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

// reloadGroupPreservePos switches to m.tabIdx's articles, saving/restoring cursor.
func (m *Model) reloadGroupPreservePos(prevTab int) {
	m.tabCursors[prevTab] = m.cursor
	m.tabOffsets[prevTab] = m.viewport.YOffset
	m.articlesOffset = 0
	_ = m.reloadArticles()
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

func (m *Model) syncViewportToCursor() {
	m.centerViewportOnCursor()
}

func (m *Model) centerViewportOnCursor() {
	m.viewport.Width = m.listWidth()
	m.viewport.Height = m.contentHeight()
	content := m.renderArticles()
	cardCenter := m.cursor*linesPerSlot + linesPerCard/2
	offset := cardCenter - m.viewport.Height/2
	if offset < 0 {
		offset = 0
	}
	totalLines := strings.Count(content, "\n") + 1
	maxOffset := totalLines - m.viewport.Height
	if maxOffset < 0 {
		maxOffset = 0
	}
	if offset > maxOffset {
		offset = maxOffset
	}
	m.viewport.SetContent(content)
	m.viewport.SetYOffset(offset)
}

func (m *Model) contentHeight() int {
	if m.mode == modeCommand || m.mode == modeSuggestInput {
		return m.height - 4 - inputBoxHeight
	}
	if m.status != "" {
		return m.height - 5
	}
	return m.height - 4
}

func (m *Model) resizeViewport() {
	h := m.contentHeight()
	m.viewport.Width = m.listWidth()
	m.viewport.Height = h
	m.detailVP.Width = m.detailPaneWidth()
	m.detailVP.Height = h
	m.centerViewportOnCursor()
	m.updateDetailContent()
}

func (m *Model) updateDetailContent() {
	if !m.detailOpen || m.detailPaneWidth() <= 0 {
		return
	}
	m.detailVP.SetYOffset(0)
	m.detailVP.SetContent(m.renderDetailContent())
}

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
