package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/kumagaias/tailfeed/internal/db"
)

// ── parseSuggestJSON ──────────────────────────────────────────────────────────

func TestParseSuggestJSON_valid(t *testing.T) {
	input := `{"feeds":[{"title":"HN","url":"https://news.ycombinator.com/rss","description":"tech news"}]}`
	feeds, err := parseSuggestJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 1 {
		t.Fatalf("want 1 feed, got %d", len(feeds))
	}
	if feeds[0].Title != "HN" || feeds[0].URL != "https://news.ycombinator.com/rss" {
		t.Errorf("unexpected feed: %+v", feeds[0])
	}
}

func TestParseSuggestJSON_withProse(t *testing.T) {
	// AI sometimes wraps JSON in prose text.
	input := `Here are some feeds: {"feeds":[{"title":"Go Blog","url":"https://go.dev/blog/feed.atom","description":"official"}]} enjoy!`
	feeds, err := parseSuggestJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 1 || feeds[0].URL != "https://go.dev/blog/feed.atom" {
		t.Errorf("unexpected: %+v", feeds)
	}
}

func TestParseSuggestJSON_noJSON(t *testing.T) {
	_, err := parseSuggestJSON("no json here")
	if err == nil {
		t.Fatal("expected error for missing JSON")
	}
}

func TestParseSuggestJSON_skipsEmptyURL(t *testing.T) {
	input := `{"feeds":[{"title":"No URL","url":""},{"title":"Valid","url":"https://example.com/feed"}]}`
	feeds, err := parseSuggestJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 1 || feeds[0].URL != "https://example.com/feed" {
		t.Errorf("expected empty-URL entry skipped, got %+v", feeds)
	}
}

// ── stripHTML ─────────────────────────────────────────────────────────────────

func TestStripHTML(t *testing.T) {
	cases := []struct{ in, want string }{
		{"<p>Hello <b>world</b></p>", "Hello world"},
		{"no tags", "no tags"},
		{"<br/>", ""},
		{"<a href='x'>link</a> text", "link text"},
	}
	for _, c := range cases {
		got := stripHTML(c.in)
		if got != c.want {
			t.Errorf("stripHTML(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ── truncate ─────────────────────────────────────────────────────────────────

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("got %q", got)
	}
	got := truncate("hello world", 7)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("expected ellipsis, got %q", got)
	}
	// Multibyte: each Japanese character is width 2.
	got = truncate("あいうえお", 4)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("expected truncation of wide chars, got %q", got)
	}
}

// ── wordWrap ──────────────────────────────────────────────────────────────────

func TestWordWrap(t *testing.T) {
	out := wordWrap("hello world foo", 8)
	lines := strings.Split(out, "\n")
	for _, l := range lines {
		if len([]rune(l)) > 8 {
			t.Errorf("line too long: %q", l)
		}
	}
}

func TestWordWrap_longWord(t *testing.T) {
	out := wordWrap("abcdefghij", 4)
	lines := strings.Split(out, "\n")
	for _, l := range lines {
		if len([]rune(l)) > 4 {
			t.Errorf("line exceeds width: %q", l)
		}
	}
}

func TestWordWrap_emptyWidth(t *testing.T) {
	s := "hello"
	if got := wordWrap(s, 0); got != s {
		t.Errorf("zero width should return original, got %q", got)
	}
}

// ── summary HTML ─────────────────────────────────────────────────────────────

func TestBuildSummaryHTML_linksSummaryHeadingsWithoutArticleIndex(t *testing.T) {
	html := buildSummaryHTML("## 1. Example A\nTL;DR", []db.Article{
		{Title: "Example A", Link: "https://example.com/a"},
	})

	if !strings.Contains(html, `<h2><a href="https://example.com/a" target="_blank">1. Example A</a></h2>`) {
		t.Fatalf("expected summary heading to link to article, got:\n%s", html)
	}
	if strings.Contains(html, `<div class="articles">`) || strings.Contains(html, "📰 Articles") {
		t.Fatalf("expected no separate article index, got:\n%s", html)
	}
	if got := strings.Count(html, `href="https://example.com/a"`); got != 1 {
		t.Fatalf("expected one article link, got %d", got)
	}
}

func TestBuildSummaryHTML_linksFourthLevelArticleHeadings(t *testing.T) {
	html := buildSummaryHTML("#### 1. Example A\nTL;DR", []db.Article{
		{Title: "Example A", Link: "https://example.com/a"},
	})

	if !strings.Contains(html, `<h4><a href="https://example.com/a" target="_blank">1. Example A</a></h4>`) {
		t.Fatalf("expected h4 summary heading to link to article, got:\n%s", html)
	}
}

func TestBuildSummaryHTML_dropsRawArticleURLLine(t *testing.T) {
	html := buildSummaryHTML("## 1. Example A\nURL: https://example.com/a\nTL;DR", []db.Article{
		{Title: "Example A", Link: "https://example.com/a"},
	})

	if strings.Contains(html, "URL:") {
		t.Fatalf("expected raw URL label to be removed, got:\n%s", html)
	}
	if got := strings.Count(html, `https://example.com/a`); got != 1 {
		t.Fatalf("expected URL only in title link, got %d occurrences:\n%s", got, html)
	}
}

func TestBuildSummaryHTML_stripsSummaryLabelsAndLinksBoldListTitle(t *testing.T) {
	html := buildSummaryHTML("1. **Example A**\n- **TL;DR:** Concise summary\n- Key Points:\n  - Point 1", []db.Article{
		{Title: "Example A", Link: "https://example.com/a"},
	})

	for _, bad := range []string{"TL;DR", "Key Points", "**"} {
		if strings.Contains(html, bad) {
			t.Fatalf("expected %q to be stripped, got:\n%s", bad, html)
		}
	}
	for _, want := range []string{
		`<li><a href="https://example.com/a" target="_blank">Example A</a>`,
		"<li>Concise summary",
		"<li>Point 1",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected %q in html, got:\n%s", want, html)
		}
	}
}

func TestBuildSummaryHTML_rendersOrderedMarkdownLinks(t *testing.T) {
	html := buildSummaryHTML("1. [Example A](https://example.com/a)\n  - Concise summary\n    - Point 1\n    - Point 2", nil)

	for _, want := range []string{
		"<ol>",
		`<li><a href="https://example.com/a" target="_blank">Example A</a>`,
		"<ul>",
		"<li>Concise summary",
		"<li>Point 1",
		"<li>Point 2",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected %q in html, got:\n%s", want, html)
		}
	}
}

func TestBuildSummaryHTML_preservesOrderedListStart(t *testing.T) {
	html := buildSummaryHTML("2. [Example B](https://example.com/b)\n  - Concise summary\n    - Point 1\n    - Point 2", nil)

	if !strings.Contains(html, `<ol start="2">`) {
		t.Fatalf("expected ordered list start to be preserved, got:\n%s", html)
	}
}

// ── humanTime ────────────────────────────────────────────────────────────────

func TestHumanTime_nil(t *testing.T) {
	if got := humanTime(nil); got != "unknown" {
		t.Errorf("got %q", got)
	}
}

func TestHumanTime_recent(t *testing.T) {
	now := time.Now()
	if got := humanTime(&now); got != "just now" {
		t.Errorf("got %q", got)
	}
}

func TestHumanTime_hours(t *testing.T) {
	t2 := time.Now().Add(-3 * time.Hour)
	got := humanTime(&t2)
	if got != "3h ago" {
		t.Errorf("got %q", got)
	}
}

func TestHumanTime_days(t *testing.T) {
	t2 := time.Now().Add(-48 * time.Hour)
	got := humanTime(&t2)
	if got != "2d ago" {
		t.Errorf("got %q", got)
	}
}

// ── visLen ────────────────────────────────────────────────────────────────────

func TestVisLen(t *testing.T) {
	// Plain string: visLen == len.
	if got := visLen("hello"); got != 5 {
		t.Errorf("got %d", got)
	}
	// ANSI escape should be stripped.
	styled := "\x1b[1mhello\x1b[0m"
	if got := visLen(styled); got != 5 {
		t.Errorf("expected 5, got %d", got)
	}
}
