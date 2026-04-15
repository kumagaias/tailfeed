package tui

import (
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
)

const articlesLimit = 200

// linesPerCard is the fixed visual height of every card (border×2 + title + meta + summary).
// linesPerSlot includes the blank separator line between cards.
const linesPerCard = 5
const linesPerSlot = linesPerCard + 1

// groupTab is the "All" virtual tab plus real DB groups.
type groupTab struct {
	id   *int64 // nil = "All"
	name string
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
	cursor   int
	viewport viewport.Model
	input    textinput.Model
	status   string // transient status message

	// feedListItems holds rendered lines for the feed list overlay.
	feedListItems []string

	// pendingG is true after a single "g" press, waiting for a second to form "gg".
	pendingG bool
}

// New creates the initial TUI model.
func New(database *db.DB, poller *feed.Poller) (*Model, error) {
	ti := textinput.New()
	ti.Placeholder = "add <url>  |  remove <url>  |  group new <name>  |  group del <name>"
	ti.CharLimit = 512

	m := &Model{
		db:     database,
		poller: poller,
		input:  ti,
	}
	if err := m.reloadTabs(); err != nil {
		return nil, err
	}
	if err := m.reloadArticles(); err != nil {
		return nil, err
	}
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
	tabs := []groupTab{{id: nil, name: "All"}}
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
	var groupID *int64
	if m.tabIdx > 0 && m.tabIdx < len(m.tabs) {
		groupID = m.tabs[m.tabIdx].id
	}
	articles, err := m.db.ListArticles(groupID, articlesLimit)
	if err != nil {
		return err
	}
	m.articles = articles
	// clamp cursor; caller is responsible for positioning after reload
	if m.cursor >= len(m.articles) {
		m.cursor = max(0, len(m.articles)-1)
	}
	return nil
}

// jumpToNewest moves the cursor to the newest article (last in ASC list) and scrolls to bottom.
func (m *Model) jumpToNewest() {
	if len(m.articles) > 0 {
		m.cursor = len(m.articles) - 1
	}
	m.viewport.SetContent(m.renderArticles())
	m.viewport.GotoBottom()
}

// jumpToOldest moves the cursor to the oldest article (first in ASC list) and scrolls to top.
func (m *Model) jumpToOldest() {
	m.cursor = 0
	m.viewport.SetContent(m.renderArticles())
	m.viewport.GotoTop()
}

func (m *Model) currentGroupID() *int64 {
	if m.tabIdx == 0 || m.tabIdx >= len(m.tabs) {
		return nil
	}
	return m.tabs[m.tabIdx].id
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
