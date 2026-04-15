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

// listenForArticles converts poller channel into Bubble Tea messages.
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
		m.viewport = viewport.New(msg.Width, m.contentHeight())
		m.viewport.SetContent(m.renderArticles())
		return m, nil

	case newArticleMsg:
		_ = m.reloadArticles()
		m.viewport.SetContent(m.renderArticles())
		// keep listening
		return m, listenForArticles(m.poller.Articles())

	case errMsg:
		m.status = "error: " + msg.err.Error()
		return m, nil

	case tea.KeyMsg:
		if m.mode == modeCommand {
			return m.updateCommand(msg)
		}
		return m.updateNormal(msg)
	}
	return m, nil
}

func (m *Model) updateNormal(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit

	case key.Matches(msg, keys.Command):
		m.mode = modeCommand
		m.input.SetValue("")
		m.input.Focus()
		return m, textinput.Blink

	case key.Matches(msg, keys.Left):
		if m.tabIdx > 0 {
			m.tabIdx--
			m.cursor = 0
			_ = m.reloadArticles()
			m.viewport.SetContent(m.renderArticles())
			m.viewport.GotoTop()
		}

	case key.Matches(msg, keys.Right):
		if m.tabIdx < len(m.tabs)-1 {
			m.tabIdx++
			m.cursor = 0
			_ = m.reloadArticles()
			m.viewport.SetContent(m.renderArticles())
			m.viewport.GotoTop()
		}

	case key.Matches(msg, keys.Up):
		if m.cursor > 0 {
			m.cursor--
			m.syncViewport()
		}

	case key.Matches(msg, keys.Down):
		if m.cursor < len(m.articles)-1 {
			m.cursor++
			m.syncViewport()
		}

	case key.Matches(msg, keys.Open):
		if m.cursor < len(m.articles) {
			link := m.articles[m.cursor].Link
			if link != "" {
				_ = openBrowser(link)
			}
		}

	case key.Matches(msg, keys.MarkRead):
		if m.cursor < len(m.articles) {
			_ = m.db.MarkRead(m.articles[m.cursor].ID)
			_ = m.reloadArticles()
			m.viewport.SetContent(m.renderArticles())
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
		return m, nil

	case key.Matches(msg, keys.Confirm):
		cmd := strings.TrimSpace(m.input.Value())
		m.mode = modeNormal
		m.input.Blur()
		m.status = m.execCommand(cmd)
		m.viewport.SetContent(m.renderArticles())
		return m, nil
	}

	var tiCmd tea.Cmd
	m.input, tiCmd = m.input.Update(msg)
	return m, tiCmd
}

// execCommand parses and runs a "/" command, returning a status message.
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
	case "help", "?":
		return "commands: add <url>  remove <url>  group new <name>  group del <name>"
	}
	return fmt.Sprintf("unknown command: %s — type help for usage", parts[0])
}

func (m *Model) cmdAdd(args []string) string {
	if len(args) == 0 {
		return "usage: add <url> [--group <name>]"
	}
	url := args[0]
	groupID := m.currentGroupID()

	// parse --group flag
	for i, a := range args[1:] {
		if a == "--group" && i+2 < len(args) {
			name := args[i+2]
			g, err := m.db.GetGroupByName(name)
			if err != nil {
				return fmt.Sprintf("group %q not found", name)
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
	if len(args) == 0 {
		return "usage: group del <name>"
	}
	name := strings.Join(args, " ")
	g, err := m.db.GetGroupByName(name)
	if err != nil {
		return fmt.Sprintf("group %q not found", name)
	}
	if err := m.db.DeleteGroup(g.ID); err != nil {
		return "error: " + err.Error()
	}
	_ = m.reloadTabs()
	_ = m.reloadArticles()
	return fmt.Sprintf("deleted group %q", name)
}

func (m *Model) syncViewport() {
	m.viewport.SetContent(m.renderArticles())
	// scroll so cursor card is visible
	lineHeight := cardHeight + 1
	targetY := m.cursor * lineHeight
	if targetY < m.viewport.YOffset {
		m.viewport.SetYOffset(targetY)
	} else if targetY+lineHeight > m.viewport.YOffset+m.viewport.Height {
		m.viewport.SetYOffset(targetY + lineHeight - m.viewport.Height)
	}
}

func (m *Model) contentHeight() int {
	// header (1) + tab bar (1) + separator (1) + footer (2)
	return m.height - 5
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
