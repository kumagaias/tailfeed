package summary

import (
	"strings"
	"testing"

	"github.com/kumagaias/tailfeed/internal/db"
)

func TestCompleteArticleBlocksKeepsOnlyCompleteBlocks(t *testing.T) {
	articles := []db.Article{
		{Title: "First", Link: "https://example.com/1"},
		{Title: "Second", Link: "https://example.com/2"},
	}
	text := `1. [wrong](https://wrong.example/1)
  - Summary
    - Point 1
    - Point 2

2. [wrong](https://wrong.example/2)
  - Summary only`

	got, completed := completeArticleBlocks(text, articles, 3)
	if completed != 1 {
		t.Fatalf("completed = %d, want 1", completed)
	}
	if !strings.Contains(got, "3. [First](https://example.com/1)") {
		t.Fatalf("expected canonical first article link, got:\n%s", got)
	}
	if strings.Contains(got, "Second") {
		t.Fatalf("expected incomplete second article to be excluded, got:\n%s", got)
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
	got := prompt("today", 2, "Japanese", "AI infra")
	for _, want := range []string{
		`Write all generated summary content in Japanese`,
		`Keep original article titles exactly as provided`,
		`## 今日の要点`,
		`## 重要記事`,
		`Start with a 今日の要点 section at the very top`,
		`User theme: AI infra`,
		`Do not write a separate URL line under article titles`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in prompt, got:\n%s", want, got)
		}
	}
}

func TestPromptUsesEnglishHeadingsForEnglish(t *testing.T) {
	got := prompt("today", 2, "English", "")
	for _, want := range []string{
		`Write all generated summary content in English`,
		`## Executive Summary`,
		`## Important Articles`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected %q in prompt, got:\n%s", want, got)
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
