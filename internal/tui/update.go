package tui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/kumagaias/tailfeed/internal/db"
	"github.com/kumagaias/tailfeed/internal/feed"
)

// newArticleMsg is sent when the poller saves a new article.
type newArticleMsg db.Article

// errMsg wraps an error for display.
type errMsg struct{ err error }

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

	case tea.MouseMsg:
		if msg.Type == tea.MouseLeft && m.mode == modeNormal {
			// Tab bar is at row 1 — switch group.
			if msg.Y == 1 {
				for i, r := range m.tabXRanges() {
					if msg.X >= r[0] && msg.X < r[1] && i != m.tabIdx {
						m.tabIdx = i
						_ = m.reloadArticles()
						m.centerViewportOnCursor()
						m.updateDetailContent()
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
					// Click on the indicator/heart position (title line, col 2) → toggle stock.
					if lineInSlot == 1 && msg.X == 2 {
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
			m.input.Placeholder = "add <url>  |  remove <url>  |  list  |  group new <name>  |  group del"
		} else {
			m.input.Placeholder = "add <url>  |  remove <url>  |  list  |  group new <name>  |  group del <name>"
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
		if m.cursor < len(m.articles)-1 {
			m.cursor++
			m.detailOpen = true
			m.resizeViewport()
			m.syncViewportToCursor()
			m.updateDetailContent()
		}

	case key.Matches(msg, keys.PrevGroup):
		if m.tabIdx > 0 {
			m.tabIdx--
			_ = m.reloadArticles()
			m.centerViewportOnCursor()
			m.updateDetailContent()
		}

	case key.Matches(msg, keys.NextGroup):
		if m.tabIdx < len(m.tabs)-1 {
			m.tabIdx++
			_ = m.reloadArticles()
			m.centerViewportOnCursor()
			m.updateDetailContent()
		}

	case key.Matches(msg, keys.Up):
		if m.cursor > 0 {
			m.cursor--
			m.syncViewportToCursor()
			m.updateDetailContent()
		}

	case key.Matches(msg, keys.Down):
		if m.cursor < len(m.articles)-1 {
			m.cursor++
			m.syncViewportToCursor()
			m.updateDetailContent()
		}

	case key.Matches(msg, keys.ViewDetail):
		m.detailOpen = !m.detailOpen
		m.resizeViewport()
		m.updateDetailContent()

	case key.Matches(msg, keys.Open):
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
		m.status = m.execCommand(cmd)
		m.resizeViewport()
		return m, nil
	}

	var tiCmd tea.Cmd
	m.input, tiCmd = m.input.Update(msg)
	return m, tiCmd
}

// scrolloff is the minimum number of lines kept above/below the cursor card.
const scrolloff = 2

// syncViewportToCursor re-renders and scrolls just enough to keep the cursor
// card visible with scrolloff padding (vim-style). Only G/gg force centering.
func (m *Model) syncViewportToCursor() {
	m.viewport.SetContent(m.renderArticles())

	cardTop := m.cursor * linesPerSlot
	cardBot := cardTop + linesPerCard // exclusive bottom line of card

	top := m.viewport.YOffset
	bot := top + m.viewport.Height

	if cardTop-scrolloff < top {
		// card is above visible area → scroll up
		m.viewport.SetYOffset(max(0, cardTop-scrolloff))
	} else if cardBot+scrolloff > bot {
		// card is below visible area → scroll down
		m.viewport.SetYOffset(cardBot + scrolloff - m.viewport.Height)
	}
	// otherwise card is already fully visible → no scroll
}

// centerViewportOnCursor scrolls so the cursor card is exactly in the middle.
// Used by G and gg.
func (m *Model) centerViewportOnCursor() {
	m.viewport.SetContent(m.renderArticles())
	cardTop := m.cursor * linesPerSlot
	center := cardTop + linesPerCard/2
	offset := center - m.viewport.Height/2
	if offset < 0 {
		offset = 0
	}
	m.viewport.SetYOffset(offset)
}

func (m *Model) contentHeight() int {
	// Rows consumed outside the viewport:
	//   header\n + tabbar\n + separator\n  = 3 rows above
	//   \n (before footer) + footer        = 2 rows below  → total chrome = 5
	// Command mode: footer = inputBoxHeight rows instead of 1
	if m.mode == modeCommand {
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

func (m *Model) execCommand(raw string) string {
	parts := strings.Fields(raw)
	if len(parts) == 0 {
		return ""
	}
	switch parts[0] {
	case "add":
		return m.cmdAdd(parts[1:])
	case "remove", "rm":
		return m.cmdRemove(parts[1:])
	case "group":
		if len(parts) < 2 {
			return "usage: group new <name> | group del <name>"
		}
		switch parts[1] {
		case "new":
			return m.cmdGroupNew(parts[2:])
		case "del", "delete":
			return m.cmdGroupDel(parts[2:])
		}
	case "list", "ls":
		return m.cmdList()
	case "help", "?":
		return m.cmdHelp()
	}
	return fmt.Sprintf("unknown command: %s — type help for usage", parts[0])
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
		"  list                  manage feeds",
		"  group new <name>      create group",
		"  group del [name]      delete group (omit name = current group)",
		"  help                  show this help",
		"",
		"  q / ctrl+c    quit",
	}
	m.feedListFeeds = nil
	m.feedListCursor = 0
	m.mode = modeFeedList
	return ""
}

// reloadFeedList refreshes feedListItems and feedListFeeds without touching cursor or mode.
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
