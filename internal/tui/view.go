package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"

	"github.com/kumagaias/tailfeed/internal/db"
)

// ── Styles ────────────────────────────────────────────────────────────────────

var (
	styleBrand = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("12"))

	styleTabActive = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("15")).
			Background(lipgloss.Color("4")).
			Padding(0, 1)

	styleTabInactive = lipgloss.NewStyle().
				Foreground(lipgloss.Color("8")).
				Padding(0, 1)

	styleCardSelected = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("12")). // bright blue
				Padding(0, 1)

	styleCardNormal = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("238")). // subtle dark grey
			Padding(0, 1)

	styleCardRead = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("238")).
			Foreground(lipgloss.Color("8")).
			Padding(0, 1)

	styleCursorBar = lipgloss.NewStyle().
			Foreground(lipgloss.Color("12")).
			Bold(true)

	styleHeart = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)

	styleTitle = lipgloss.NewStyle().Bold(true)

	styleMeta = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	styleSummary = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))

	styleStatus = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))

	styleHelp = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	styleInput = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("4")).
			Padding(0, 1)
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
	return brand + styleMeta.Render(count)
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

	hint := styleHelp.Render("[^H⇧←  groups  ]$L⇧→")
	pad := m.width - visLen(tabs) - visLen(hint)
	if pad > 0 {
		tabs += strings.Repeat(" ", pad) + hint
	}
	return tabs
}

// tabXRanges returns the [start, end) X column range of each tab in the tab bar.
// Each tab is rendered with 1-cell padding on each side, tabs separated by 1 space.
func (m *Model) tabXRanges() [][2]int {
	ranges := make([][2]int, len(m.tabs))
	x := 0
	for i, t := range m.tabs {
		w := utf8.RuneCountInString(t.name) + 2 // +2 for left/right padding
		ranges[i] = [2]int{x, x + w}
		x += w + 1 // +1 for the space separator between tabs
	}
	return ranges
}

// renderArticles renders all cards. Each card occupies exactly linesPerSlot lines
// (linesPerCard visible + 1 blank separator), so cursor offsets are predictable.
func (m *Model) renderArticles() string {
	if len(m.articles) == 0 {
		return styleMeta.Render("\n  No articles yet. Add a feed with /add <url>\n")
	}
	innerWidth := m.listWidth() - 4 // border (1+1) + padding (1+1)
	if innerWidth < 10 {
		innerWidth = 10
	}
	var b strings.Builder
	for i, a := range m.articles {
		b.WriteString(m.renderCard(i, a, innerWidth))
		b.WriteString("\n") // blank separator → total = linesPerSlot per card
	}
	return b.String()
}

// renderCard renders a single article card.
// Content is exactly 3 lines (title + meta + summary) so the card
// occupies a predictable linesPerCard=5 lines (borders included).
func (m *Model) renderCard(idx int, a db.Article, width int) string {
	selected := idx == m.cursor

	// inner content width (card border=2, padding=2 → 4 chars total)
	inner := width - 2 // reserve 2 for "▶ " / "  " prefix
	if inner < 4 {
		inner = 4
	}

	// ── Line 1: title ──────────────────────────────────────────────────────
	title := truncate(a.Title, inner)
	var indicator string
	switch {
	case selected && a.IsStocked:
		indicator = styleCursorBar.Render("▶") + styleHeart.Render("♥")
		title = styleCursorBar.Render(title)
	case selected:
		indicator = styleCursorBar.Render("▶ ")
		title = styleCursorBar.Render(title)
	case a.IsStocked:
		indicator = styleHeart.Render("♥ ")
		if !a.IsRead {
			title = styleTitle.Render(title)
		}
	default:
		indicator = "  "
		if !a.IsRead {
			title = styleTitle.Render(title)
		}
	}

	// ── Line 2: meta ────────────────────────────────────────────────────────
	meta := styleMeta.Render(truncate(a.FeedTitle+"  ·  "+humanTime(a.PublishedAt), width-2))

	// ── Line 3: summary (one line) ──────────────────────────────────────────
	summaryFull := strings.Join(strings.Fields(stripHTML(a.Summary)), " ")
	summaryLine := " "
	if summaryFull != "" {
		runes := []rune(summaryFull)
		if len(runes) <= inner {
			summaryLine = summaryFull
		} else {
			summaryLine = string(runes[:inner-1]) + "…"
		}
	}

	content := indicator + title + "\n" +
		"  " + meta + "\n" +
		"  " + styleSummary.Render(summaryLine)

	var s lipgloss.Style
	switch {
	case selected:
		s = styleCardSelected.Width(width)
	case a.IsRead:
		s = styleCardRead.Width(width)
	default:
		s = styleCardNormal.Width(width)
	}
	return s.Render(content)
}

var styleFeedList = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(lipgloss.Color("4")).
	Padding(0, 1)

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
			// Help lines: section headers are unindented and bold, others indented.
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
	panel := styleFeedList.Width(m.width - 4).Render(strings.Join(lines, "\n"))
	return panel
}

func (m *Model) renderFooter() string {
	if m.mode == modeCommand {
		prompt := styleInput.Width(m.width - 4).Render(m.input.View())
		return prompt
	}
	if m.status != "" {
		left := styleStatus.Render(m.status)
		right := styleHelp.Render("esc clear")
		return left + strings.Repeat(" ", max(0, m.width-visLen(left)-visLen(right))) + right
	}
	if m.mode == modeFeedList {
		return ""
	}
	help := styleHelp.Render("↑↓/jk move  ←→/hl detail  [^H groups  v detail  space ♥  G newest  gg oldest  ^F/^B page  enter open  m read  / cmd  q quit")
	return help
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func humanTime(t *time.Time) string {
	if t == nil {
		return "unknown"
	}
	d := time.Since(*t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max-1]) + "…"
}

// stripHTML removes HTML tags from a string (minimal, no dependency).
func stripHTML(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

func filterEmpty(ss ...string) []string {
	var out []string
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

// renderDetailContent renders the full article detail for the right pane.
func (m *Model) renderDetailContent() string {
	if m.cursor >= len(m.articles) {
		return ""
	}
	a := m.articles[m.cursor]
	w := m.detailPaneWidth() - 2 // inner width (padding)
	if w < 10 {
		w = 10
	}

	var b strings.Builder

	// Title (word-wrapped)
	titleWrapped := wordWrap(a.Title, w)
	b.WriteString(styleTitle.Render(titleWrapped))
	b.WriteString("\n\n")

	// Meta
	b.WriteString(styleMeta.Render(truncate(a.FeedTitle+"  ·  "+humanTime(a.PublishedAt), w)))
	b.WriteString("\n\n")

	// Full summary
	summary := strings.Join(strings.Fields(stripHTML(a.Summary)), " ")
	if summary != "" {
		b.WriteString(styleSummary.Render(wordWrap(summary, w)))
		b.WriteString("\n\n")
	}

	// Link
	if a.Link != "" {
		b.WriteString(styleMeta.Render(truncate(a.Link, w)))
		b.WriteString("\n\n")
	} else {
		b.WriteString("\n\n")
	}

	// Stock hint
	if a.IsStocked {
		b.WriteString(styleHelp.Render("space ") + styleHeart.Render("♥") + styleHelp.Render(" unstock  /  click ♥ to toggle"))
	} else {
		b.WriteString(styleHelp.Render("space ♡ stock  /  click ♥ to toggle"))
	}

	return lipgloss.NewStyle().Padding(0, 1).Render(b.String())
}

// wordWrap breaks s into lines of at most width runes, breaking at word boundaries.
func wordWrap(s string, width int) string {
	if width <= 0 {
		return s
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return s
	}
	var lines []string
	var cur strings.Builder
	curLen := 0
	for _, w := range words {
		wLen := utf8.RuneCountInString(w)
		if curLen == 0 {
			cur.WriteString(w)
			curLen = wLen
		} else if curLen+1+wLen <= width {
			cur.WriteString(" ")
			cur.WriteString(w)
			curLen += 1 + wLen
		} else {
			lines = append(lines, cur.String())
			cur.Reset()
			cur.WriteString(w)
			curLen = wLen
		}
	}
	if curLen > 0 {
		lines = append(lines, cur.String())
	}
	return strings.Join(lines, "\n")
}

// visLen returns the visual length of a styled string (strips ANSI escape codes).
func visLen(s string) int {
	inEsc := false
	n := 0
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		n++
	}
	return n
}
