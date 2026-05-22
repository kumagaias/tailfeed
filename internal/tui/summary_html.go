package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/kumagaias/tailfeed/internal/db"
)

// WriteSummaryHTML generates an HTML file from the MCP summary text and article list,
// writes it to a temp file, opens it in the browser, and returns the file path.
func WriteSummaryHTML(summaryText string, articles []db.Article) (string, error) {
	return writeSummaryHTML(summaryText, articles)
}

// writeSummaryHTML is the internal implementation.
func writeSummaryHTML(summaryText string, articles []db.Article) (string, error) {
	path := fmt.Sprintf("%s/summary-%s.html", os.TempDir(), time.Now().Format("2006-01-02-150405"))
	if err := os.WriteFile(path, []byte(buildSummaryHTML(summaryText, articles)), 0644); err != nil {
		return "", err
	}
	return path, nil
}

func buildSummaryHTML(summaryText string, articles []db.Article) string {
	var b strings.Builder

	b.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>tailfeed — Daily Summary</title>
<style>
  :root { --bg: #0d1117; --fg: #e6edf3; --muted: #8b949e; --accent: #58a6ff; --border: #30363d; --card: #161b22; }
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { background: var(--bg); color: var(--fg); font-family: ui-monospace, 'Cascadia Code', 'Fira Code', monospace; font-size: 14px; line-height: 1.6; padding: 2rem; max-width: 860px; margin: 0 auto; }
  h1 { color: var(--accent); font-size: 1.4rem; margin-bottom: 0.25rem; }
  .meta { color: var(--muted); font-size: 0.85rem; margin-bottom: 2rem; }
  h2 { color: var(--accent); font-size: 1rem; margin: 2rem 0 0.5rem; border-bottom: 1px solid var(--border); padding-bottom: 0.25rem; }
  h3 { color: var(--fg); font-size: 0.95rem; margin: 1.5rem 0 0.5rem; }
  p { margin: 0.5rem 0; color: var(--fg); }
  ol, ul { margin: 0.5rem 0 0.5rem 1.5rem; }
  li { margin: 0.2rem 0; }
  a { color: var(--accent); text-decoration: none; }
  a:hover { text-decoration: underline; }
  pre { white-space: pre-wrap; word-break: break-word; }
</style>
</head>
<body>
`)

	b.WriteString(fmt.Sprintf("<h1>📡 tailfeed — Daily Summary</h1>\n"))
	b.WriteString(fmt.Sprintf(`<div class="meta">%s · %d articles</div>`+"\n",
		time.Now().Format("2006-01-02 Mon"), len(articles)))

	// Render summary markdown as simple HTML
	b.WriteString(markdownToHTML(summaryText, articles))
	b.WriteString("</body></html>\n")
	return b.String()
}

// markdownToHTML converts a minimal subset of Markdown to HTML.
// Articles are used to linkify headings that match article titles.
func markdownToHTML(md string, articles []db.Article) string {
	// Build a lookup: lowercase title words → URL
	titleURL := make(map[string]string, len(articles))
	for _, a := range articles {
		if a.Link != "" {
			titleURL[summaryHeadingKey(a.Title)] = a.Link
		}
	}

	// findArticleURL returns the URL for a heading text if it matches an article title.
	findArticleURL := func(heading string) string {
		h := summaryHeadingKey(heading)
		// Exact match first.
		if u, ok := titleURL[h]; ok {
			return u
		}
		// Partial match: heading contains article title or vice versa.
		for title, u := range titleURL {
			if strings.Contains(h, title) || strings.Contains(title, h) {
				return u
			}
		}
		return ""
	}

	var b strings.Builder
	lines := strings.Split(md, "\n")
	lists := make([]listFrame, 0, 4)
	for _, line := range lines {
		if typ, indent, number, content, ok := parseListItem(line); ok {
			closeListsTo(&b, &lists, typ, indent, number)
			b.WriteString("<li>" + formatInline(content) + "\n")
			continue
		}

		switch {
		case strings.HasPrefix(line, "## "):
			closeAllLists(&b, &lists)
			text := line[3:]
			if u := findArticleURL(text); u != "" {
				b.WriteString(`<h2><a href="` + htmlEscape(u) + `" target="_blank">` + htmlEscape(text) + `</a></h2>` + "\n")
			} else {
				b.WriteString("<h2>" + formatInline(text) + "</h2>\n")
			}
		case strings.HasPrefix(line, "### "):
			closeAllLists(&b, &lists)
			b.WriteString("<h3>" + formatInline(line[4:]) + "</h3>\n")
		case strings.TrimSpace(line) == "":
			closeAllLists(&b, &lists)
			b.WriteString("\n")
		default:
			closeAllLists(&b, &lists)
			b.WriteString("<p>" + formatInline(line) + "</p>\n")
		}
	}
	closeAllLists(&b, &lists)
	return b.String()
}

type listFrame struct {
	typ    string
	indent int
}

func parseListItem(line string) (string, int, int, string, bool) {
	indent := 0
	pos := 0
	for pos < len(line) && (line[pos] == ' ' || line[pos] == '\t') {
		if line[pos] == '\t' {
			indent += 2
		} else {
			indent++
		}
		pos++
	}
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
		return "ul", indent, 0, strings.TrimSpace(trimmed[2:]), true
	}

	i := 0
	for i < len(trimmed) && trimmed[i] >= '0' && trimmed[i] <= '9' {
		i++
	}
	if i > 0 && i+1 < len(trimmed) && trimmed[i] == '.' && trimmed[i+1] == ' ' {
		number := 1
		_, _ = fmt.Sscanf(trimmed[:i], "%d", &number)
		return "ol", indent, number, strings.TrimSpace(trimmed[i+2:]), true
	}
	return "", 0, 0, "", false
}

func closeListsTo(b *strings.Builder, lists *[]listFrame, typ string, indent int, number int) {
	for len(*lists) > 0 {
		top := (*lists)[len(*lists)-1]
		if top.indent < indent || (top.indent == indent && top.typ == typ) {
			break
		}
		b.WriteString("</li>\n</" + top.typ + ">\n")
		*lists = (*lists)[:len(*lists)-1]
	}
	if len(*lists) == 0 || (*lists)[len(*lists)-1].indent < indent || (*lists)[len(*lists)-1].typ != typ {
		if typ == "ol" && number > 1 {
			b.WriteString(fmt.Sprintf(`<ol start="%d">`+"\n", number))
		} else {
			b.WriteString("<" + typ + ">\n")
		}
		*lists = append(*lists, listFrame{typ: typ, indent: indent})
		return
	}
	b.WriteString("</li>\n")
}

func closeAllLists(b *strings.Builder, lists *[]listFrame) {
	for len(*lists) > 0 {
		top := (*lists)[len(*lists)-1]
		b.WriteString("</li>\n</" + top.typ + ">\n")
		*lists = (*lists)[:len(*lists)-1]
	}
}

func summaryHeadingKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Trim(s, "#*[]()「」『』\"'")
	s = strings.TrimSpace(s)

	for len(s) > 0 {
		i := 0
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
		}
		if i == 0 || i >= len(s) {
			break
		}
		switch s[i] {
		case '.', ')', ':':
			s = strings.TrimSpace(s[i+1:])
			continue
		}
		break
	}

	return strings.Join(strings.Fields(s), " ")
}

func formatInline(s string) string {
	var b strings.Builder
	for {
		open := strings.Index(s, "[")
		if open < 0 {
			b.WriteString(autoLink(htmlEscape(s)))
			break
		}
		close := strings.Index(s[open:], "](")
		if close < 0 {
			b.WriteString(autoLink(htmlEscape(s)))
			break
		}
		close += open
		end := strings.Index(s[close+2:], ")")
		if end < 0 {
			b.WriteString(autoLink(htmlEscape(s)))
			break
		}
		end += close + 2

		b.WriteString(autoLink(htmlEscape(s[:open])))
		text := s[open+1 : close]
		url := s[close+2 : end]
		if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
			b.WriteString(`<a href="` + htmlEscape(url) + `" target="_blank">` + htmlEscape(text) + `</a>`)
		} else {
			b.WriteString(htmlEscape(s[open : end+1]))
		}
		s = s[end+1:]
	}
	return b.String()
}

// autoLink converts plain URLs (already HTML-escaped, so https://... or http://...)
// into clickable anchor tags. Must be called after htmlEscape.
func autoLink(s string) string {
	const httpPrefix = "http://"
	const httpsPrefix = "https://"
	var b strings.Builder
	for {
		idx := strings.Index(s, httpsPrefix)
		if i := strings.Index(s, httpPrefix); i >= 0 && (idx < 0 || i < idx) {
			idx = i
		}
		if idx < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:idx])
		rest := s[idx:]
		// Find end of URL: space or common trailing punctuation
		end := strings.IndexAny(rest, " \t\n\"'<>)")
		var url string
		if end < 0 {
			url = rest
			s = ""
		} else {
			url = rest[:end]
			s = rest[end:]
		}
		// Strip trailing punctuation like . or ,
		url = strings.TrimRight(url, ".,;:")
		b.WriteString(`<a href="` + url + `" target="_blank">` + url + `</a>`)
	}
	return b.String()
}

func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}
