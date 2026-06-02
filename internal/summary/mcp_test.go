package summary

import (
	"strings"
	"testing"

	"github.com/kumagaias/tailfeed/internal/db"
)

func TestCompleteArticleBlocksFillsIncompleteBlocks(t *testing.T) {
	articles := []db.Article{
		{Title: "First", Link: "https://example.com/1", Summary: "First summary"},
		{Title: "Second", Link: "https://example.com/2", Summary: "Second summary"},
	}
	text := `1. [wrong](https://wrong.example/1)
  - Summary
    - Point 1
    - Point 2

2. [wrong](https://wrong.example/2)
  - Summary only`

	got, completed := completeArticleBlocks(text, articles, 3)
	if completed != 2 {
		t.Fatalf("completed = %d, want 2", completed)
	}
	if !strings.Contains(got, "3. [First](https://example.com/1)") {
		t.Fatalf("expected canonical first article link, got:\n%s", got)
	}
	if !strings.Contains(got, "4. [Second](https://example.com/2)") {
		t.Fatalf("expected incomplete second article to be retained, got:\n%s", got)
	}
	if !strings.Contains(got, "Summary only") {
		t.Fatalf("expected incomplete content to be preserved, got:\n%s", got)
	}
}

func TestCompleteArticleBlocksFillsMissingArticles(t *testing.T) {
	articles := []db.Article{
		{Title: "First", Link: "https://example.com/1", Summary: "First summary"},
		{Title: "Second", Link: "https://example.com/2", Summary: "Second summary"},
	}
	text := `1. [wrong](https://wrong.example/1)
  - Summary
    - Point 1
    - Point 2`

	got, completed := completeArticleBlocks(text, articles, 10)
	if completed != 2 {
		t.Fatalf("completed = %d, want 2", completed)
	}
	if !strings.Contains(got, "10. [First](https://example.com/1)") || !strings.Contains(got, "11. [Second](https://example.com/2)") {
		t.Fatalf("expected both articles to be completed, got:\n%s", got)
	}
	if !strings.Contains(got, "AI 出力に含まれなかったため自動補完しました。") {
		t.Fatalf("expected missing article fallback reason, got:\n%s", got)
	}
}

func TestExtractExecutiveSummary(t *testing.T) {
	text := `## 今日の要点
- 生成AI関連の更新が目立つ
- 開発者向け機能の改善が続く

1. [wrong](https://wrong.example/1)
  - Summary
    - Point 1
    - Point 2`

	got := extractExecutiveSummary(text)
	for _, want := range []string{
		"- 生成AI関連の更新が目立つ",
		"- 開発者向け機能の改善が続く",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in executive summary, got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "wrong") {
		t.Fatalf("expected article list to be excluded, got:\n%s", got)
	}
}

func TestExtractImportantArticles(t *testing.T) {
	text := `## 今日の要点
- 全体傾向

## 重要記事
- [Important](https://example.com/i) - 影響が大きい
- [Second](https://example.com/s) - 実装判断に関係

1. [wrong](https://wrong.example/1)
  - Summary
    - Point 1
    - Point 2`

	got := extractImportantArticles(text)
	for _, want := range []string{
		"- [Important](https://example.com/i) - 影響が大きい",
		"- [Second](https://example.com/s) - 実装判断に関係",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in important articles, got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "wrong") {
		t.Fatalf("expected article list to be excluded, got:\n%s", got)
	}
}

func TestPromptRequiresJapaneseExecutiveSummaryFirst(t *testing.T) {
	got := prompt("today", 2, "Japanese", "AI <infra>")
	for _, want := range []string{
		`<summary_request>`,
		`<language>Japanese</language>`,
		`<rule>Write every generated heading, sentence, reason, and bullet in Japanese.</rule>`,
		`## 今日の要点`,
		`## 重要記事`,
		`<theme>AI &lt;infra&gt;</theme>`,
		`Do not write separate URL lines.`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in prompt, got:\n%s", want, got)
		}
	}
}

func TestPromptUsesEnglishHeadingsForEnglish(t *testing.T) {
	got := prompt("today", 2, "English", "")
	for _, want := range []string{
		`<language>English</language>`,
		`<theme>No user theme is set.</theme>`,
		`## Executive Summary`,
		`## Important Articles`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in prompt, got:\n%s", want, got)
		}
	}
}

func TestBuildContextUsesXMLAndEscapesArticleFields(t *testing.T) {
	got := buildContext([]db.Article{{
		Title:   "A < B",
		Link:    "https://example.com/?a=1&b=2",
		Summary: "Use <tag> safely",
	}})

	for _, want := range []string{
		`<articles>`,
		`<article index="1">`,
		`<title>A &lt; B</title>`,
		`<url>https://example.com/?a=1&amp;b=2</url>`,
		`<summary>Use safely</summary>`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in XML context, got:\n%s", want, got)
		}
	}
}

func TestCompleteArticleBlocksDropsRawURLLine(t *testing.T) {
	articles := []db.Article{
		{Title: "First", Link: "https://example.com/1"},
	}
	text := `1. [wrong](https://wrong.example/1)
URL: https://example.com/1
  - Summary
    - Point 1
    - Point 2`

	got, completed := completeArticleBlocks(text, articles, 1)
	if completed != 1 {
		t.Fatalf("completed = %d, want 1", completed)
	}
	if strings.Contains(got, "URL:") || strings.Contains(got, "\nhttps://example.com/1") {
		t.Fatalf("expected raw URL line to be removed, got:\n%s", got)
	}
}

func TestNextChunkRespectsMaxRunes(t *testing.T) {
	articles := []db.Article{
		{Title: "First", Link: "https://example.com/1", Summary: strings.Repeat("a", 20)},
		{Title: "Second", Link: "https://example.com/2", Summary: strings.Repeat("b", 20)},
		{Title: "Third", Link: "https://example.com/3", Summary: strings.Repeat("c", 20)},
	}

	chunk, rest := nextChunk(articles, 90)
	if len(chunk) != 1 || len(rest) != 2 {
		t.Fatalf("got chunk=%d rest=%d, want chunk=1 rest=2", len(chunk), len(rest))
	}
}

func TestNextChunkCapsArticlesPerAttempt(t *testing.T) {
	articles := make([]db.Article, MaxArticlesPerAttempt+2)
	for i := range articles {
		articles[i] = db.Article{
			Title:   "Article",
			Link:    "https://example.com",
			Summary: "short",
		}
	}

	chunk, rest := nextChunk(articles, 10000)
	if len(chunk) != MaxArticlesPerAttempt || len(rest) != 2 {
		t.Fatalf("got chunk=%d rest=%d, want chunk=%d rest=2", len(chunk), len(rest), MaxArticlesPerAttempt)
	}
}

func TestLimitArticlesCapsAtSummaryLimit(t *testing.T) {
	articles := make([]db.Article, MaxSummaryArticles+1)

	got := LimitArticles(articles)
	if len(got) != MaxSummaryArticles {
		t.Fatalf("len = %d, want %d", len(got), MaxSummaryArticles)
	}
}

func TestBuildDigestUsesFiveLinesAtTop(t *testing.T) {
	articles := make([]db.Article, 6)
	for i := range articles {
		articles[i] = db.Article{
			Title:   "Article",
			Summary: "GitHub Copilot and AWS Lambda release update",
		}
	}

	got := buildDigest(articles, "Japanese")
	if !strings.HasPrefix(got, "## 本日のダイジェスト\n") {
		t.Fatalf("expected Japanese digest heading, got:\n%s", got)
	}
	if !strings.Contains(got, "最新 6 件の要約") {
		t.Fatalf("expected latest article scope, got:\n%s", got)
	}
	lines := strings.Split(got, "\n")
	if len(lines) != 7 {
		t.Fatalf("digest lines = %d, want 7 including heading and scope:\n%s", len(lines), got)
	}
	if strings.Contains(got, "ほか") || strings.Contains(got, "last 24 hours") {
		t.Fatalf("expected no remaining article count line, got:\n%s", got)
	}
	if strings.Contains(got, "「Article」") || strings.Contains(got, "GitHub Copilot and AWS") {
		t.Fatalf("expected digest to avoid raw article/title excerpts, got:\n%s", got)
	}
	for _, want := range []string{
		"生成AIやエージェント活用",
		"日常的な開発環境の改善",
		"AWS やインフラ関連",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected digest topic %q, got:\n%s", want, got)
		}
	}
}

func TestFallbackBlocksPreservesLinks(t *testing.T) {
	got := fallbackBlocks([]db.Article{
		{Title: "Fallback", Link: "https://example.com/f", Summary: "<p>Hello world</p>"},
	}, 2)

	if !strings.Contains(got, "2. [Fallback](https://example.com/f)") {
		t.Fatalf("expected fallback link, got:\n%s", got)
	}
	if !strings.Contains(got, "詳細は元記事を確認してください。") {
		t.Fatalf("expected Japanese fallback content, got:\n%s", got)
	}
}
