package summary

import (
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/kumagaias/tailfeed/internal/db"
	"github.com/kumagaias/tailfeed/internal/mcp"
)

const (
	DefaultMaxContextRunes = 6000
	MaxMCPAttempts         = 5
	MaxSummaryArticles     = 50
	MaxArticlesPerAttempt  = 20
	ArticleSummaryRunes    = 160
)

var (
	htmlTagRE     = regexp.MustCompile(`<[^>]+>`)
	whitespaceRE  = regexp.MustCompile(`\s+`)
	listHeadingRE = regexp.MustCompile(`^\d+\.\s+`)
	bulletRE      = regexp.MustCompile(`^\s+-\s+`)
)

func SummarizeWithMCP(cfg *mcp.Config, label string, articles []db.Article, maxContextRunes int, theme string) (string, error) {
	return SummarizeWithMCPInLanguage(cfg, label, articles, maxContextRunes, theme, cfg.SummaryLanguage())
}

func SummarizeWithMCPInLanguage(cfg *mcp.Config, label string, articles []db.Article, maxContextRunes int, theme string, language string) (string, error) {
	return SummarizeWithMCPInLanguageWithProgress(cfg, label, articles, maxContextRunes, theme, language, nil)
}

func SummarizeWithMCPInLanguageWithProgress(cfg *mcp.Config, label string, articles []db.Article, maxContextRunes int, theme string, language string, progress ProgressFunc) (string, error) {
	articles = LimitArticles(articles)
	if maxContextRunes <= 0 {
		maxContextRunes = DefaultMaxContextRunes
	}
	language = strings.TrimSpace(language)
	if language == "" {
		language = "Japanese"
	}
	executiveHeading, importantHeading := summaryHeadings(language)

	var out []string
	var executive []string
	var important []string
	pending := append([]db.Article(nil), articles...)
	nextNumber := 1

	for attempt := 1; attempt <= MaxMCPAttempts && len(pending) > 0; attempt++ {
		chunk, rest := nextChunk(pending, maxContextRunes)
		if len(chunk) == 0 {
			chunk = pending[:1]
			rest = pending[1:]
		}
		notifySummaryProgress(progress, "start", attempt, MaxMCPAttempts, nextNumber, len(chunk), len(articles))

		text, err := mcp.Call(cfg, map[string]any{
			"question": prompt(label, len(chunk), language, theme),
			"context":  buildContext(chunk),
		})
		if err != nil {
			return "", err
		}
		notifySummaryProgress(progress, "done", attempt, MaxMCPAttempts, nextNumber, len(chunk), len(articles))

		if exec := extractExecutiveSummary(text); exec != "" {
			executive = append(executive, exec)
		}
		if imp := extractImportantArticles(text); imp != "" {
			important = append(important, imp)
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
		if len(important) == 0 {
			return prependDigest(body, label, articles, language), nil
		}
		return prependDigest("## "+importantHeading+"\n"+strings.Join(important, "\n")+"\n\n"+body, label, articles, language), nil
	}
	result := "## " + executiveHeading + "\n" + strings.Join(executive, "\n")
	if len(important) > 0 {
		result += "\n\n## " + importantHeading + "\n" + strings.Join(important, "\n")
	}
	return prependDigest(result+"\n\n"+body, label, articles, language), nil
}

func LimitArticles(articles []db.Article) []db.Article {
	if len(articles) <= MaxSummaryArticles {
		return articles
	}
	return articles[:MaxSummaryArticles]
}

func prependDigest(text string, _ string, articles []db.Article, language string) string {
	digest := buildDigest(articles, language)
	text = strings.TrimSpace(text)
	if text == "" {
		return digest
	}
	return digest + "\n\n" + text
}

func buildDigest(articles []db.Article, language string) string {
	heading := "Today's Digest"
	scope := fmt.Sprintf("Latest %d articles summarized.", len(articles))
	if isJapaneseLanguage(language) {
		heading = "本日のダイジェスト"
		scope = fmt.Sprintf("最新 %d 件の要約", len(articles))
	}
	var b strings.Builder
	b.WriteString("## " + heading + "\n")
	b.WriteString(scope + "\n")
	if len(articles) == 0 {
		if isJapaneseLanguage(language) {
			b.WriteString("対象記事はありません。")
		} else {
			b.WriteString("No articles to summarize.")
		}
		return b.String()
	}
	lines := englishDigestLines(articles)
	if isJapaneseLanguage(language) {
		lines = japaneseDigestLines(articles)
	}
	for _, line := range lines {
		b.WriteString(line + "\n")
	}
	return strings.TrimSpace(b.String())
}

func japaneseDigestLines(articles []db.Article) []string {
	topics := digestTopics(articles)
	lines := []string{
		"開発現場で使うツール、AI、クラウド運用の更新が中心です。",
	}
	if topics["ai"] > 0 {
		lines = append(lines, "生成AIやエージェント活用は、開発体験に近い領域へ広がっています。")
	}
	if topics["devtools"] > 0 {
		lines = append(lines, "ターミナル、エディタ、GitHub 周辺など日常的な開発環境の改善が目立ちます。")
	}
	if topics["cloud"] > 0 {
		lines = append(lines, "AWS やインフラ関連では、運用しやすさと移行のしやすさが焦点です。")
	}
	if topics["security"] > 0 {
		lines = append(lines, "セキュリティや依存関係管理は、実装判断に直結する話題として扱われています。")
	}
	if topics["release"] > 0 {
		lines = append(lines, "新機能やリリース情報は、すぐ試せる実務寄りの内容が多めです。")
	}
	if len(lines) < 5 {
		lines = append(lines, "個別記事は、技術選定や日々のワークフロー改善の材料として読めます。")
	}
	if len(lines) < 5 {
		lines = append(lines, "全体として、導入判断に必要な変化を短時間で把握できる構成です。")
	}
	return lines[:min(5, len(lines))]
}

func englishDigestLines(articles []db.Article) []string {
	topics := digestTopics(articles)
	lines := []string{
		"Today's articles focus on developer tools, AI, and cloud operations.",
	}
	if topics["ai"] > 0 {
		lines = append(lines, "AI and agent workflows continue moving closer to everyday development work.")
	}
	if topics["devtools"] > 0 {
		lines = append(lines, "Terminals, editors, GitHub, and local tooling show practical workflow improvements.")
	}
	if topics["cloud"] > 0 {
		lines = append(lines, "Cloud and infrastructure updates emphasize operations, migration, and maintainability.")
	}
	if topics["security"] > 0 {
		lines = append(lines, "Security and dependency topics remain relevant to implementation decisions.")
	}
	if topics["release"] > 0 {
		lines = append(lines, "Release notes and new features provide concrete items to evaluate or try.")
	}
	if len(lines) < 5 {
		lines = append(lines, "The set is useful for scanning changes that may affect technical choices.")
	}
	if len(lines) < 5 {
		lines = append(lines, "Overall, it favors practical updates over broad industry commentary.")
	}
	return lines[:min(5, len(lines))]
}

func digestTopics(articles []db.Article) map[string]int {
	topics := map[string]int{
		"ai":       0,
		"devtools": 0,
		"cloud":    0,
		"security": 0,
		"release":  0,
	}
	for _, a := range articles {
		text := strings.ToLower(a.Title + " " + PlainText(a.Summary, 160))
		for topic, keywords := range map[string][]string{
			"ai":       {"ai", "chatgpt", "copilot", "agent", "llm", "生成ai", "人工知能"},
			"devtools": {"github", "git", "terminal", "editor", "ide", "cli", "alacritty", "ターミナル", "エディタ"},
			"cloud":    {"aws", "lambda", "cloud", "kubernetes", "docker", "infra", "インフラ", "クラウド"},
			"security": {"security", "vulnerability", "cve", "auth", "サイバー", "脆弱", "認証"},
			"release":  {"release", "launch", "update", "project", "新機能", "リリース", "公開"},
		} {
			for _, keyword := range keywords {
				if strings.Contains(text, keyword) {
					topics[topic]++
					break
				}
			}
		}
	}
	return topics
}

func prompt(label string, articleCount int, language string, theme string) string {
	theme = strings.TrimSpace(theme)
	themeXML := "<theme>No user theme is set.</theme>"
	if theme != "" {
		themeXML = fmt.Sprintf("<theme>%s</theme>", xmlEscape(theme))
	}
	executiveHeading, importantHeading := summaryHeadings(language)
	return fmt.Sprintf(`<summary_request>
  <role>You are a senior engineer's daily briefing assistant.</role>
  <task>Summarize the provided articles for a technical audience.</task>
  <inputs>
    <period>%s</period>
    <article_count>%d</article_count>
    <language>%s</language>
    %s
  </inputs>
  <rules>
    <rule>Write every generated heading, sentence, reason, and bullet in %s.</rule>
    <rule>Keep original article titles exactly as provided, even when titles use another language.</rule>
    <rule>Include every article from the XML context exactly once in the numbered article list.</rule>
    <rule>Use numbered Markdown links for article titles. The title link is the only URL location.</rule>
    <rule>Do not write separate URL lines.</rule>
    <rule>Do not write labels such as TL;DR, Summary, Key Points, or 要約 before summary sentences.</rule>
    <rule>If a user theme is set, use it to prioritize the executive summary and important articles, but do not omit any article.</rule>
    <rule>Do not add sections other than the exact Markdown contract below.</rule>
  </rules>
  <output_contract format="markdown">
## %s
- Executive summary point 1 in %s, max 55 characters
- Executive summary point 2 in %s, max 55 characters

## %s
- [Original article title](article URL) - reason in %s, max 45 characters
- [Original article title](article URL) - reason in %s, max 45 characters

1. [Original article title](article URL)
  - Summary sentence in %s, max 60 characters
    - Key technical point 1 in %s, max 45 characters
    - Key technical point 2 in %s, max 45 characters
  </output_contract>
</summary_request>`, xmlEscape(label), articleCount, xmlEscape(language), themeXML, xmlEscape(language), executiveHeading, xmlEscape(language), xmlEscape(language), importantHeading, xmlEscape(language), xmlEscape(language), xmlEscape(language), xmlEscape(language), xmlEscape(language))
}

func summaryHeadings(language string) (string, string) {
	if isJapaneseLanguage(language) {
		return "今日の要点", "重要記事"
	}
	return "Executive Summary", "Important Articles"
}

func isJapaneseLanguage(language string) bool {
	language = strings.ToLower(strings.TrimSpace(language))
	return language == "" || strings.Contains(language, "japanese") || strings.Contains(language, "日本")
}

func buildContext(articles []db.Article) string {
	var b strings.Builder
	b.WriteString("<articles>\n")
	for i, a := range articles {
		b.WriteString(fmt.Sprintf(`  <article index="%d">
    <title>%s</title>
    <url>%s</url>
    <summary>%s</summary>
  </article>
`, i+1, xmlEscape(a.Title), xmlEscape(a.Link), xmlEscape(PlainText(a.Summary, ArticleSummaryRunes))))
	}
	b.WriteString("</articles>")
	return b.String()
}

func xmlEscape(s string) string {
	return html.EscapeString(strings.TrimSpace(s))
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
	for i := range articles {
		if i >= len(blocks) {
			appendFallbackArticleBlock(&out, articles[i], startNumber+i, "AI 出力に含まれなかったため自動補完しました。")
			completed++
			continue
		}
		block := blocks[i]
		if len(block) == 0 {
			break
		}
		if completed > 0 {
			out.WriteString("\n\n")
		}
		out.WriteString(fmt.Sprintf("%d. [%s](%s)\n", startNumber+i, articles[i].Title, articles[i].Link))
		wroteContent := false
		if bulletCount(block) > 0 {
			for _, line := range block[1:] {
				if strings.HasPrefix(strings.TrimSpace(line), "#") {
					break
				}
				if isArticleURLLine(line, articles[i].Link) {
					continue
				}
				if strings.TrimSpace(line) != "" {
					wroteContent = true
				}
				out.WriteString(line)
				out.WriteByte('\n')
			}
		}
		if !wroteContent {
			appendFallbackArticleDetails(&out, articles[i], "AI 出力が不完全だったため自動補完しました。")
		}
		completed++
	}

	return strings.TrimSpace(out.String()), completed
}

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
