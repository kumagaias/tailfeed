package tui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/kumagaias/tailfeed/internal/db"
)

// View renders the full UI.
func (m *Model) View() string {
	if m.width == 0 {
		return "loading…"
	}
	var b strings.Builder
	b.WriteString(m.renderHeader())
	b.WriteString("\n")
	b.WriteString(m.renderTabBar())
	b.WriteString("\n")
	b.WriteString(strings.Repeat("─", m.width))
	b.WriteString("\n")
	if m.mode == modeFeedList {
		b.WriteString(m.renderFeedList())
	} else if m.mode == modeSuggest {
		b.WriteString(m.renderSuggestList())
	} else if m.detailOpen && m.detailPaneWidth() > 0 {
		h := m.contentHeight()
		divider := strings.Repeat("│\n", h-1) + "│"
		body := lipgloss.JoinHorizontal(lipgloss.Top,
			m.viewport.View(),
			divider,
			m.detailVP.View(),
		)
		b.WriteString(body)
	} else {
		b.WriteString(m.viewport.View())
	}
	b.WriteString("\n")
	b.WriteString(m.renderFooter())
	return b.String()
}

func (m *Model) renderHeader() string {
	brand := styleBrand.Render("tailfeed")
	count := fmt.Sprintf("  %d articles", len(m.articles))
	left := brand + styleMeta.Render(count)
	help := styleHelp.Render("↑↓/jk move  ←→/hl detail  space ♥  G/gg  [ ] groups  / cmd  q quit")
	pad := m.width - visLen(left) - visLen(help)
	if pad > 0 {
		return left + strings.Repeat(" ", pad) + help
	}
	return left
}

func (m *Model) renderTabBar() string {
	var parts []string
	for i, t := range m.tabs {
		label := t.name
		if i == m.tabIdx {
			parts = append(parts, styleTabActive.Render(label))
		} else {
			parts = append(parts, styleTabInactive.Render(label))
		}
	}
	tabs := strings.Join(parts, " ")

	var searchText string
	if m.mode == modeFind && m.input.Value() != "" {
		searchText = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Render("🔍 " + m.input.Value() + "▌")
	} else if m.mode == modeFind {
		searchText = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Render("🔍 ▌")
	} else {
		searchText = styleHelp.Render("🔍 search...")
	}
	right := "[" + searchText + "]"
	pad := m.width - lipgloss.Width(tabs) - lipgloss.Width(right)
	if pad > 0 {
		tabs += strings.Repeat(" ", pad) + right
	}
	return tabs
}

// tabXRanges returns the [start, end) X column range of each tab in the tab bar.
func (m *Model) tabXRanges() [][2]int {
	ranges := make([][2]int, len(m.tabs))
	x := 0
	for i, t := range m.tabs {
		w := utf8.RuneCountInString(t.name) + 2
		ranges[i] = [2]int{x, x + w}
		x += w + 1
	}
	return ranges
}

// renderArticles renders all cards.
func (m *Model) renderArticles() string {
	if len(m.articles) == 0 {
		return styleMeta.Render("\n  No articles yet. Add a feed with /add <url>\n")
	}
	innerWidth := m.listWidth() - 4
	if innerWidth < 10 {
		innerWidth = 10
	}
	var b strings.Builder
	for i, a := range m.articles {
		b.WriteString(m.renderCard(i, a, innerWidth))
		if i < len(m.articles)-1 {
			b.WriteString("\n\n")
		}
	}
	return b.String()
}

// renderCard renders a single article card.
func (m *Model) renderCard(idx int, a db.Article, width int) string {
	selected := idx == m.cursor
	inner := width - 3
	if inner < 4 {
		inner = 4
	}

	title := truncate(a.Title, inner)
	cursor := " "
	if selected {
		cursor = styleCursorBar.Render("▶")
		title = styleCursorBar.Render(title)
	} else if !a.IsRead {
		title = styleTitle.Render(title)
	}
	heart := styleHeartEmpty.Render("❤")
	if a.IsStocked {
		heart = styleHeart.Render("❤")
	}
	indicator := cursor + heart + " "

	meta := styleMeta.Inline(true).MaxWidth(width - 2).Render(truncate(a.FeedTitle+"  ·  "+humanTime(a.PublishedAt), width-2))

	summaryFull := strings.Join(strings.Fields(stripHTML(a.Summary)), " ")
	summaryLine := " "
	if summaryFull != "" {
		summaryLine = truncate(summaryFull, inner)
	}

	content := indicator + title + "\n" +
		"  " + meta + "\n" +
		"  " + styleSummary.MaxWidth(inner).Render(summaryLine)

	var s lipgloss.Style
	switch {
	case selected:
		s = styleCardSelected.Width(width + 2)
	case a.IsRead:
		s = styleCardRead.Width(width + 2)
	default:
		s = styleCardNormal.Width(width + 2)
	}
	rendered := s.Render(content)
	lines := strings.Split(rendered, "\n")
	if len(lines) > linesPerCard {
		lines = lines[:linesPerCard]
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderFeedList() string {
	isHelp := m.feedListFeeds == nil && len(m.feedListItems) > 0

	var title string
	if isHelp {
		title = styleCursorBar.Render("Help — keybindings & commands")
	} else {
		title = styleCursorBar.Render(fmt.Sprintf("Registered feeds (%d)", len(m.feedListItems)))
	}
	lines := []string{title, ""}

	for i, item := range m.feedListItems {
		if isHelp {
			if item == "" {
				lines = append(lines, "")
			} else if len(item) > 0 && item[0] != ' ' {
				lines = append(lines, styleTitle.Render(item))
			} else {
				lines = append(lines, styleMeta.Render(item))
			}
		} else {
			prefix := "  "
			text := truncate(item, m.width-10)
			if i == m.feedListCursor {
				prefix = styleCursorBar.Render("▶ ")
				text = styleTitle.Render(text)
			}
			lines = append(lines, prefix+text)
		}
	}

	if !isHelp && len(m.feedListItems) == 0 {
		lines = append(lines, styleMeta.Render("  No feeds registered."))
	}

	if m.feedListConfirm {
		lines = append(lines, "", lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true).Render("  Delete this feed? y/enter to confirm, any other key to cancel"))
	} else {
		if isHelp {
			lines = append(lines, "", styleHelp.Render("  esc / q  close"))
		} else {
			lines = append(lines, "", styleHelp.Render("  ↑↓/jk select  d delete  esc/q close"))
		}
	}
	return styleFeedList.Width(m.width - 4).Render(strings.Join(lines, "\n"))
}

func (m *Model) renderSuggestList() string {
	selected := len(m.suggestSelected)
	var titleStr string
	if selected > 0 {
		titleStr = styleCursorBar.Render(fmt.Sprintf("Suggested feeds (%d)  — %d selected", len(m.suggestFeeds), selected))
	} else {
		titleStr = styleCursorBar.Render(fmt.Sprintf("Suggested feeds (%d)", len(m.suggestFeeds)))
	}
	footer := styleHelp.Render("  ↑↓/jk move  space select  enter add selected (or current)  esc cancel")
	maxItems := (m.height - 6) / 4
	if maxItems < 1 {
		maxItems = 1
	}

	start := m.suggestCursor - maxItems/2
	if start < 0 {
		start = 0
	}
	end := start + maxItems
	if end > len(m.suggestFeeds) {
		end = len(m.suggestFeeds)
		start = end - maxItems
		if start < 0 {
			start = 0
		}
	}

	lines := []string{titleStr, ""}
	for i := start; i < end; i++ {
		f := m.suggestFeeds[i]
		check := styleHeartEmpty.Render("[ ]")
		if m.suggestSelected[i] {
			check = styleHeart.Render("[✓]")
		}
		titleText := truncate(f.Title, m.width-12)
		urlText := styleMeta.Render("    " + truncate(f.URL, m.width-12))
		descText := ""
		if f.Description != "" {
			descText = styleSummary.Render("    " + truncate(f.Description, m.width-12))
		}
		if i == m.suggestCursor {
			titleText = styleTitle.Render(titleText)
		}
		var line string
		if i == m.suggestCursor {
			line = styleCursorBar.Render("▶") + " " + check + " " + titleText
		} else {
			line = "  " + check + " " + titleText
		}
		lines = append(lines, line, urlText)
		if descText != "" {
			lines = append(lines, descText)
		}
		lines = append(lines, "")
	}
	lines = append(lines, footer)
	return styleFeedList.Width(m.width - 4).Render(strings.Join(lines, "\n"))
}

func (m *Model) renderFooter() string {
	if m.mode == modeCommand || m.mode == modeSuggestInput {
		return styleInput.Width(m.width - 4).Render(m.input.View())
	}
	if m.status != "" {
		left := styleStatus.Render(m.status)
		right := styleHelp.Render("esc clear")
		return left + strings.Repeat(" ", max(0, m.width-visLen(left)-visLen(right))) + right
	}
	return ""
}

// renderDetailContent renders the full article detail for the right pane.
func (m *Model) renderDetailContent() string {
	if m.cursor >= len(m.articles) {
		return ""
	}
	a := m.articles[m.cursor]
	w := m.detailPaneWidth() - 2
	if w < 10 {
		w = 10
	}

	var b strings.Builder
	b.WriteString(styleTitle.Render(wordWrap(a.Title, w)))
	b.WriteString("\n\n")

	if m.mcpResult != "" {
		b.WriteString(styleSummary.Render(wordWrap(m.mcpResult, w)))
		return lipgloss.NewStyle().Padding(0, 1).Render(b.String())
	}

	b.WriteString(styleMeta.Render(truncate(a.FeedTitle+"  ·  "+humanTime(a.PublishedAt), w)))
	b.WriteString("\n\n")

	summary := strings.Join(strings.Fields(stripHTML(a.Summary)), " ")
	if summary != "" {
		b.WriteString(styleSummary.Render(wordWrap(summary, w)))
		b.WriteString("\n\n")
	}

	if a.Link != "" {
		b.WriteString(styleMeta.Render(truncate(a.Link, w)))
		b.WriteString("\n\n")
	} else {
		b.WriteString("\n\n")
	}

	if a.IsStocked {
		b.WriteString(styleHelp.Render("space ") + styleHeart.Render("♥") + styleHelp.Render(" unstock  /  click ♥ to toggle"))
	} else {
		b.WriteString(styleHelp.Render("space ♡ stock  /  click ♥ to toggle"))
	}

	return lipgloss.NewStyle().Padding(0, 1).Render(b.String())
}
