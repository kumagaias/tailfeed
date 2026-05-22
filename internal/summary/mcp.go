package summary

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/kumagaias/tailfeed/internal/db"
	"github.com/kumagaias/tailfeed/internal/mcp"
)

const (
	DefaultMaxContextRunes = 6000
	MaxMCPAttempts         = 3
	MaxArticlesPerAttempt  = 8
	ArticleSummaryRunes    = 160
)

var (
	htmlTagRE     = regexp.MustCompile(`<[^>]+>`)
	whitespaceRE  = regexp.MustCompile(`\s+`)
	listHeadingRE = regexp.MustCompile(`^\d+\.\s+`)
	bulletRE      = regexp.MustCompile(`^\s+-\s+`)
)

func SummarizeWithMCP(cfg *mcp.Config, label string, articles []db.Article, maxContextRunes int) (string, error) {
	if maxContextRunes <= 0 {
		maxContextRunes = DefaultMaxContextRunes
	}

	var out []string
	var executive []string
	pending := append([]db.Article(nil), articles...)
	nextNumber := 1

	for attempt := 1; attempt <= MaxMCPAttempts && len(pending) > 0; attempt++ {
		chunk, rest := nextChunk(pending, maxContextRunes)
		if len(chunk) == 0 {
			chunk = pending[:1]
			rest = pending[1:]
		}

		text, err := mcp.Call(cfg, map[string]any{
			"question": prompt(label, len(chunk), cfg.SummaryLanguage()),
			"context":  buildContext(chunk),
		})
		if err != nil {
			return "", err
		}

		if exec := extractExecutiveSummary(text); exec != "" {
			executive = append(executive, exec)
		}

		normalized, completed := completeArticleBlocks(text, chunk, nextNumber)
		if strings.TrimSpace(normalized) != "" {
			out = append(out, strings.TrimSpace(normalized))
		}
		if completed > len(chunk) {
			completed = len(chunk)
		}
		nextNumber += completed

		pending = append(append([]db.Article(nil), chunk[completed:]...), rest...)
	}

	if len(pending) > 0 {
		out = append(out, fallbackBlocks(pending, nextNumber))
	}

	body := strings.Join(out, "\n\n")
	if len(executive) == 0 {
		return body, nil
	}
	return "## Executive Summary\n" + strings.Join(executive, "\n") + "\n\n" + body, nil
}

func prompt(label string, articleCount int, language string) string {
	return fmt.Sprintf(`You are a senior engineer's daily briefing assistant. Summarize %s's %d articles in %s for a technical audience.
Write all content in Japanese, except keep the heading "Executive Summary" in English.
Start with an executive summary section at the very top. Then include every article exactly once. Keep the whole response compact. Use exactly this Markdown shape:
## Executive Summary
- Japanese executive summary point 1, max 55 characters
- Japanese executive summary point 2, max 55 characters

1. [Article title](article URL)
  - Japanese summary sentence, max 60 characters. Do not write a label like "Summary", "TL;DR", or "要約".
    - Japanese key technical point 1, max 45 characters
    - Japanese key technical point 2, max 45 characters
Do not add any other sections.`, label, articleCount, language)
}

func buildContext(articles []db.Article) string {
	var b strings.Builder
	for _, a := range articles {
		b.WriteString(fmt.Sprintf("## %s\nURL: %s\n%s\n\n", a.Title, a.Link, PlainText(a.Summary, ArticleSummaryRunes)))
	}
	return b.String()
}

func nextChunk(articles []db.Article, maxRunes int) ([]db.Article, []db.Article) {
	var used int
	for i, a := range articles {
		if i >= MaxArticlesPerAttempt {
			return articles[:i], articles[i:]
		}
		itemRunes := len([]rune(a.Title)) + len([]rune(a.Link)) + len([]rune(PlainText(a.Summary, ArticleSummaryRunes))) + 16
		if i > 0 && used+itemRunes > maxRunes {
			return articles[:i], articles[i:]
		}
		used += itemRunes
	}
	return articles, nil
}

func completeArticleBlocks(text string, articles []db.Article, startNumber int) (string, int) {
	lines := strings.Split(text, "\n")
	var blocks [][]string
	var current []string
	for _, line := range lines {
		if listHeadingRE.MatchString(line) && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") {
			if len(current) > 0 {
				blocks = append(blocks, current)
			}
			current = []string{line}
			continue
		}
		if len(current) > 0 {
			current = append(current, line)
		}
	}
	if len(current) > 0 {
		blocks = append(blocks, current)
	}

	var out strings.Builder
	completed := 0
	for i, block := range blocks {
		if i >= len(articles) || bulletCount(block) < 3 {
			break
		}
		if completed > 0 {
			out.WriteString("\n\n")
		}
		out.WriteString(fmt.Sprintf("%d. [%s](%s)\n", startNumber+completed, articles[i].Title, articles[i].Link))
		for _, line := range block[1:] {
			out.WriteString(line)
			out.WriteByte('\n')
		}
		completed++
	}

	return strings.TrimSpace(out.String()), completed
}

func bulletCount(lines []string) int {
	count := 0
	for _, line := range lines {
		if bulletRE.MatchString(line) {
			count++
		}
	}
	return count
}

func extractExecutiveSummary(text string) string {
	lines := strings.Split(text, "\n")
	var out []string
	inSection := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.EqualFold(trimmed, "## Executive Summary") || strings.EqualFold(trimmed, "# Executive Summary") {
			inSection = true
			continue
		}
		if !inSection {
			continue
		}
		if strings.HasPrefix(trimmed, "#") || listHeadingRE.MatchString(trimmed) {
			break
		}
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "- ") {
			out = append(out, trimmed)
		} else {
			out = append(out, "- "+trimmed)
		}
	}
	return strings.Join(out, "\n")
}

func fallbackBlocks(articles []db.Article, startNumber int) string {
	var b strings.Builder
	for i, a := range articles {
		if i > 0 {
			b.WriteString("\n\n")
		}
		s := PlainText(a.Summary, 70)
		if s == "" {
			s = "要約を生成できませんでした。"
		}
		b.WriteString(fmt.Sprintf("%d. [%s](%s)\n", startNumber+i, a.Title, a.Link))
		b.WriteString("  - " + s + "\n")
		b.WriteString("    - 詳細は元記事を確認してください。\n")
		b.WriteString("    - MCP の出力上限により自動補完しました。\n")
	}
	return b.String()
}

func PlainText(s string, maxRunes int) string {
	s = htmlTagRE.ReplaceAllString(s, " ")
	s = whitespaceRE.ReplaceAllString(strings.TrimSpace(s), " ")
	if maxRunes <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes])
}
