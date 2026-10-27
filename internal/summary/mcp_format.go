package summary

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/kumagaias/tailfeed/internal/db"
)

var (
	htmlTagRE    = regexp.MustCompile(`<[^>]+>`)
	whitespaceRE = regexp.MustCompile(`\s+`)
	bulletRE     = regexp.MustCompile(`^\s+-\s+`)
)

func appendFallbackArticleBlock(out *strings.Builder, article db.Article, number int, reason string) {
	if out.Len() > 0 {
		out.WriteString("\n\n")
	}
	out.WriteString(fmt.Sprintf("%d. [%s](%s)\n", number, article.Title, article.Link))
	appendFallbackArticleDetails(out, article, reason)
}

func appendFallbackArticleDetails(out *strings.Builder, article db.Article, reason string) {
	s := PlainText(article.Summary, 70)
	if s == "" {
		s = "要約を生成できませんでした。"
	}
	out.WriteString("  - " + s + "\n")
	out.WriteString("    - 詳細は元記事を確認してください。\n")
	out.WriteString("    - " + reason + "\n")
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
	return extractFirstMarkdownSection(text, "今日の要点", "Executive Summary")
}

func extractImportantArticles(text string) string {
	return extractFirstMarkdownSection(text, "重要記事", "Important Articles")
}

func extractFirstMarkdownSection(text string, headings ...string) string {
	for _, heading := range headings {
		if section := extractMarkdownSection(text, heading); section != "" {
			return section
		}
	}
	return ""
}

func extractMarkdownSection(text, heading string) string {
	lines := strings.Split(text, "\n")
	var out []string
	inSection := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if markdownHeadingEquals(trimmed, heading) {
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

func markdownHeadingEquals(line, heading string) bool {
	line = strings.TrimSpace(strings.TrimLeft(line, "#"))
	line = strings.TrimSpace(line)
	return strings.EqualFold(line, heading)
}

func isArticleURLLine(line, articleURL string) bool {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "- ")
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "URL:")
	line = strings.TrimPrefix(line, "Url:")
	line = strings.TrimPrefix(line, "url:")
	line = strings.TrimSpace(line)
	line = strings.Trim(line, "<>()[]")
	return articleURL != "" && line == articleURL
}

func fallbackBlocks(articles []db.Article, startNumber int) string {
	return fallbackBlocksWithReason(articles, startNumber, "MCP の出力上限により自動補完しました。")
}

func fallbackBlocksWithReason(articles []db.Article, startNumber int, reason string) string {
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
		b.WriteString("    - " + reason + "\n")
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
