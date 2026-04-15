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
	b.WriteString(m.viewport.View())
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
	return strings.Join(parts, " ")
}

// renderArticles renders all cards and updates m.cardOffsets with each card's starting line.
func (m *Model) renderArticles() string {
	if len(m.articles) == 0 {
		m.cardOffsets = nil
		return styleMeta.Render("\n  No articles yet. Add a feed with /add <url>\n")
	}
	innerWidth := m.width - 4 // border (1+1) + padding (1+1)
	if innerWidth < 10 {
		innerWidth = 10
	}

	m.cardOffsets = make([]int, len(m.articles))
	var b strings.Builder
	currentLine := 0

	for i, a := range m.articles {
		m.cardOffsets[i] = currentLine
		card := m.renderCard(i, a, innerWidth)
		// count actual rendered lines (including borders added by lipgloss)
		lineCount := strings.Count(card, "\n") + 1
		currentLine += lineCount + 1 // +1 for the trailing \n separator between cards
		b.WriteString(card)
		b.WriteString("\n")
	}
	return b.String()
}

func (m *Model) renderCard(idx int, a db.Article, width int) string {
	selected := idx == m.cursor

	// Reserve 2 chars for the cursor indicator ("▶ ")
	innerWidth := width - 2
	if innerWidth < 4 {
		innerWidth = 4
	}

	title := truncate(a.Title, innerWidth)
	if selected {
		title = styleCursorBar.Render(title)
	} else if !a.IsRead {
		title = styleTitle.Render(title)
	}

	meta := styleMeta.Render(a.FeedTitle + "  ·  " + humanTime(a.PublishedAt))

	summary := ""
	if a.Summary != "" {
		summary = styleSummary.Render(truncate(stripHTML(a.Summary), innerWidth))
	}

	// Prefix title with cursor indicator
	indicator := "  "
	if selected {
		indicator = styleCursorBar.Render("▶ ")
	}
	lines := filterEmpty(indicator+title, "  "+meta)
	if summary != "" {
		lines = append(lines, "  "+summary)
	}
	content := strings.Join(lines, "\n")

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

func (m *Model) renderFooter() string {
	if m.mode == modeCommand {
		prompt := styleInput.Width(m.width - 4).Render("> " + m.input.View())
		return prompt
	}
	if m.status != "" {
		left := styleStatus.Render(m.status)
		right := styleHelp.Render("esc clear")
		return left + strings.Repeat(" ", max(0, m.width-visLen(left)-visLen(right))) + right
	}
	help := styleHelp.Render("↑↓/jk scroll  ←→/hl groups  G newest  gg oldest  ^F/^B page  enter open  m read  / cmd  q quit")
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
