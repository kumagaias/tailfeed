package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/kumagaias/tailfeed/internal/db"
	"github.com/kumagaias/tailfeed/internal/feed"
)

// mode represents the current UI mode.
type mode int

const (
	modeNormal   mode = iota
	modeCommand       // "/" command palette active
	modeFeedList      // feed list overlay
	modeSuggest       // suggest feed selection overlay
	modeFind          // incremental keyword search
	modeSuggestInput  // free-text input for suggest theme
)

const articlesLimit = 1000

// articlesPageSize is how many older articles to load per "load more" request.
const articlesPageSize = 50

// linesPerCard is the fixed visual height of every card:
//   border(1) + title(1) + meta(1) + summary(1) + border(1) = 5
//
// linesPerSlot includes the blank separator line between cards.
const linesPerCard = 5
const linesPerSlot = linesPerCard + 1

// inputBoxHeight is the height of the command input box including its border.
const inputBoxHeight = 3 // border-top + content + border-bottom

// groupTab is the "All" virtual tab, the "♥ Stock" virtual tab, or a real DB group.
type groupTab struct {
	id      *int64 // nil for virtual tabs
	name    string
	isStock bool
}

// suggestFeed is a feed candidate returned by the AI suggest command.
type suggestFeed struct {
	Title       string
	URL         string
	Description string
}

// Model is the root Bubble Tea model.
type Model struct {
	db       *db.DB
	poller   *feed.Poller
	width    int
	height   int
	mode     mode
	tabs     []groupTab
	tabIdx   int
	articles []db.Article
	cursor     int
	viewport   viewport.Model
	input      textinput.Model
	status     string // transient status message
	detailOpen bool
	detailVP   viewport.Model

	// feedListItems holds rendered lines for the feed list overlay.
	feedListItems   []string
	feedListFeeds   []db.Feed
	feedListCursor  int
	feedListConfirm bool // true = waiting for y/enter to confirm delete

	// tabCursors remembers the cursor position for each tab index.
	tabCursors map[int]int
	// tabOffsets remembers the viewport YOffset for each tab index.
	tabOffsets map[int]int
	// articlesOffset is the DB OFFSET for the current article page (for loading older articles).
	articlesOffset int
	// mcpResult holds the latest MCP response to show in the detail pane.
	mcpResult string
	// filterQuery is the active keyword filter (empty = no filter).
	filterQuery string
	// suggestFeeds holds AI-suggested feeds for the selection overlay.
	suggestFeeds    []suggestFeed
	suggestCursor   int
	suggestSelected map[int]bool

	// pendingG is true after a single "g" press, waiting for a second to form "gg".
	pendingG bool

	// pendingGroupDel holds the group name awaiting delete confirmation ("y" to proceed).
	pendingGroupDel string

	// pendingClear is true when /clear is awaiting user confirmation ("y" to proceed).
	pendingClear bool

	// pendingHTMLPath holds a generated HTML report path awaiting user confirmation to open.
	pendingHTMLPath string
}

// commandPlaceholder returns the input placeholder text for the command palette.
func commandPlaceholder(withGroupDel bool) string {
	base := "add <url>  |  remove <url>  |  list  |  group new <name>  |  group del"
	if withGroupDel {
		base += " <name>"
	}
	base += "  |  suggest  |  summary  |  summary today"
	return base
}
func New(database *db.DB, poller *feed.Poller) (*Model, error) {
	ti := textinput.New()
	ti.Placeholder = commandPlaceholder(true)
	ti.CharLimit = 512

	m := &Model{
		db:         database,
		poller:     poller,
		input:      ti,
		tabCursors: make(map[int]int),
		tabOffsets: make(map[int]int),
	}
	if err := m.reloadTabs(); err != nil {
		return nil, err
	}
	if err := m.reloadArticles(); err != nil {
		return nil, err
	}
	m.jumpToNewest()
	return m, nil
}

// Init is called once when the program starts.
func (m *Model) Init() tea.Cmd {
	return listenForArticles(m.poller.Articles())
}

func (m *Model) reloadTabs() error {
	groups, err := m.db.ListGroups()
	if err != nil {
		return err
	}
	tabs := []groupTab{
		{name: "All"},
		{name: "♥ Stock", isStock: true},
	}
	for _, g := range groups {
		gCopy := g
		tabs = append(tabs, groupTab{id: &gCopy.ID, name: g.Name})
	}
	m.tabs = tabs
	if m.tabIdx >= len(m.tabs) {
		m.tabIdx = 0
	}
	return nil
}

func (m *Model) reloadArticles() error {
	var (
		articles []db.Article
		err      error
	)
	if m.tabIdx < len(m.tabs) && m.tabs[m.tabIdx].isStock {
		articles, err = m.db.ListStockedArticles(articlesLimit, m.articlesOffset)
	} else {
		var groupID *int64
		if m.tabIdx > 0 && m.tabIdx < len(m.tabs) && !m.tabs[m.tabIdx].isStock {
			groupID = m.tabs[m.tabIdx].id
		}
		articles, err = m.db.ListArticles(groupID, articlesLimit, m.articlesOffset)
	}
	if err != nil {
		return err
	}
	// Reverse so oldest is at top, newest at bottom.
	for i, j := 0, len(articles)-1; i < j; i, j = i+1, j-1 {
		articles[i], articles[j] = articles[j], articles[i]
	}
	m.articles = articles
	// Apply keyword filter if active.
	if m.filterQuery != "" {
		q := strings.ToLower(m.filterQuery)
		filtered := m.articles[:0]
		for _, a := range m.articles {
			if strings.Contains(strings.ToLower(a.Title), q) ||
				strings.Contains(strings.ToLower(a.Summary), q) ||
				strings.Contains(strings.ToLower(a.FeedTitle), q) {
				filtered = append(filtered, a)
			}
		}
		m.articles = filtered
	}
	if m.cursor >= len(m.articles) {
		m.cursor = max(0, len(m.articles)-1)
	}
	return nil
}

// jumpToNewest moves the cursor to the newest article and centers the viewport on it.
func (m *Model) jumpToNewest() {
	if len(m.articles) > 0 {
		m.cursor = len(m.articles) - 1
	}
	m.centerViewportOnCursor()
}

// jumpToOldest moves the cursor to the oldest article and centers the viewport on it.
func (m *Model) jumpToOldest() {
	m.cursor = 0
	m.centerViewportOnCursor()
}

func (m *Model) currentGroupID() *int64 {
	if m.tabIdx == 0 || m.tabIdx >= len(m.tabs) || m.tabs[m.tabIdx].isStock {
		return nil
	}
	return m.tabs[m.tabIdx].id
}

// listWidth returns the width of the article list pane.
func (m *Model) listWidth() int {
	if m.detailOpen {
		return m.width / 2
	}
	return m.width
}

// detailPaneWidth returns the width of the detail pane (0 when closed).
func (m *Model) detailPaneWidth() int {
	if !m.detailOpen {
		return 0
	}
	return m.width - m.listWidth() - 1 // 1 for divider
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
