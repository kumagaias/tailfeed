package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	runewidth "github.com/mattn/go-runewidth"
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
				BorderForeground(lipgloss.Color("12")).
				Padding(0, 1)

	styleCardNormal = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("238")).
			Padding(0, 1)

	styleCardRead = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("238")).
			Foreground(lipgloss.Color("8")).
			Padding(0, 1)

	styleCursorBar = lipgloss.NewStyle().
			Foreground(lipgloss.Color("12")).
			Bold(true)

	styleHeart      = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)
	styleHeartEmpty = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	styleTitle = lipgloss.NewStyle().Bold(true)

	styleMeta = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	styleSummary = lipgloss.NewStyle().Foreground(lipgloss.Color("7")).Inline(true)

	styleStatus = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))

	styleHelp = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))

	styleInput = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("4")).
			Padding(0, 1)

	styleFeedList = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("4")).
			Padding(0, 1)
)

// ── View helpers ──────────────────────────────────────────────────────────────

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
	width := 0
	for i, r := range s {
		w := runewidth.RuneWidth(r)
		if width+w > max {
			return s[:i] + "…"
		}
		width += w
	}
	return s
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
		if wLen > width {
			if curLen > 0 {
				lines = append(lines, cur.String())
				cur.Reset()
				curLen = 0
			}
			runes := []rune(w)
			for len(runes) > 0 {
				chunk := runes
				if len(chunk) > width {
					chunk = runes[:width]
				}
				lines = append(lines, string(chunk))
				runes = runes[len(chunk):]
			}
			continue
		}
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
