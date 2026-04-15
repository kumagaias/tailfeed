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
  ul { margin: 0.5rem 0 0.5rem 1.5rem; }
  li { margin: 0.2rem 0; }
  a { color: var(--accent); text-decoration: none; }
  a:hover { text-decoration: underline; }
  .articles { margin-top: 3rem; border-top: 1px solid var(--border); padding-top: 1.5rem; }
  .article { background: var(--card); border: 1px solid var(--border); border-radius: 6px; padding: 0.75rem 1rem; margin-bottom: 0.75rem; }
  .article-title { font-weight: bold; }
  .article-meta { color: var(--muted); font-size: 0.8rem; margin-top: 0.2rem; }
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

	// Article index with links
	b.WriteString(`<div class="articles">` + "\n")
	b.WriteString("<h2>📰 Articles</h2>\n")
	for _, a := range articles {
		b.WriteString(`<div class="article">` + "\n")
		if a.Link != "" {
			b.WriteString(fmt.Sprintf(`<div class="article-title"><a href="%s" target="_blank">%s</a></div>`+"\n",
				htmlEscape(a.Link), htmlEscape(a.Title)))
		} else {
			b.WriteString(fmt.Sprintf(`<div class="article-title">%s</div>`+"\n", htmlEscape(a.Title)))
		}
		meta := a.FeedTitle
		if a.PublishedAt != nil {
			meta += " · " + a.PublishedAt.Local().Format("15:04")
		}
		b.WriteString(fmt.Sprintf(`<div class="article-meta">%s</div>`+"\n", htmlEscape(meta)))
		b.WriteString("</div>\n")
	}
	b.WriteString("</div>\n")
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
			titleURL[strings.ToLower(a.Title)] = a.Link
		}
	}

	// findArticleURL returns the URL for a heading text if it matches an article title.
	findArticleURL := func(heading string) string {
		h := strings.ToLower(strings.TrimSpace(heading))
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
	inUL := false
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "## "):
			if inUL {
				b.WriteString("</ul>\n")
				inUL = false
			}
			text := line[3:]
			escaped := htmlEscape(text)
			if u := findArticleURL(text); u != "" {
				b.WriteString(`<h2><a href="` + htmlEscape(u) + `" target="_blank">` + escaped + `</a></h2>` + "\n")
			} else {
				b.WriteString("<h2>" + autoLink(escaped) + "</h2>\n")
			}
		case strings.HasPrefix(line, "### "):
			if inUL {
				b.WriteString("</ul>\n")
				inUL = false
			}
			b.WriteString("<h3>" + autoLink(htmlEscape(line[4:])) + "</h3>\n")
		case strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* "):
			if !inUL {
				b.WriteString("<ul>\n")
				inUL = true
			}
			b.WriteString("<li>" + autoLink(htmlEscape(line[2:])) + "</li>\n")
		case strings.TrimSpace(line) == "":
			if inUL {
				b.WriteString("</ul>\n")
				inUL = false
			}
			b.WriteString("\n")
		default:
			if inUL {
				b.WriteString("</ul>\n")
				inUL = false
			}
			b.WriteString("<p>" + autoLink(htmlEscape(line)) + "</p>\n")
		}
	}
	if inUL {
		b.WriteString("</ul>\n")
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
