package tui

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// execCommand dispatches a parsed command string.
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
		if len(parts) > 1 {
			switch parts[1] {
			case "today", "yesterday", "week":
				return m.cmdSummaryPeriod(parts[1])
			}
		}
		return m.cmdMCP("summary", parts[1:])
	case "mcp":
		return m.cmdMCPConfig(parts[1:]), nil
	case "usage":
		return m.cmdUsage()
	case "clear":
		return m.cmdClear(), nil
	}
	return fmt.Sprintf("unknown command: %s — type help for usage", parts[0]), nil
}

func (m *Model) cmdAdd(args []string) string {
	if len(args) == 0 {
		return "usage: add <url> [--group <name>]"
	}
	url := args[0]

	// Validate URL format
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return fmt.Sprintf("invalid URL format: %q (must start with http:// or https://)", url)
	}

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

func (m *Model) cmdGroupDel(args []string) string {
	var name string
	if len(args) > 0 {
		name = strings.Join(args, " ")
	} else {
		if m.tabIdx == 0 || m.tabIdx >= len(m.tabs) || m.tabs[m.tabIdx].isStock {
			return "cannot delete All or Stock"
		}
		name = m.tabs[m.tabIdx].name
	}
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
		"  summary yesterday     summarize all articles from yesterday (mcp required)",
		"  summary week          summarize articles from the last 7 days (mcp required)",
		"  usage                  show plan and remaining API quota",
		"  mcp set <cmd> [args...]  register MCP server",
		"  mcp list              list configured MCP server",
		"  mcp on / mcp off      enable or disable MCP",
		"  clear                 remove all feeds, articles, and groups",
		"  help                  show this help",
		"",
		"  q / ctrl+c    quit",
	}
	m.feedListFeeds = nil
	m.feedListCursor = 0
	m.mode = modeFeedList
	return ""
}

// cmdClear sets the pendingClear flag and shows a confirmation prompt.
func (m *Model) cmdClear() string {
	m.pendingClear = true
	return "clear all feeds, articles, and groups? press y to confirm, esc to cancel"
}

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
