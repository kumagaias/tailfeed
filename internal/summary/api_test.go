package summary

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kumagaias/tailfeed/internal/api"
	"github.com/kumagaias/tailfeed/internal/db"
)

func TestSummarizeWithAPILimitsArticlesAndPrependsDigest(t *testing.T) {
	orig := apiSummary
	defer func() { apiSummary = orig }()

	calls := 0
	apiSummary = func(_ string, articles []api.SummaryArticle, _ string, _ string) (string, error) {
		calls++
		var b strings.Builder
		for i, a := range articles {
			if i > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(fmt.Sprintf("%d. [%s](%s)\n", i+1, a.Title, a.URL))
			b.WriteString("  - 要約\n")
			b.WriteString("    - ポイント1\n")
			b.WriteString("    - ポイント2\n")
		}
		b.WriteString("\n\n## Today's Signal\n不要なセクション")
		return b.String(), nil
	}

	articles := make([]db.Article, MaxSummaryArticles+2)
	for i := range articles {
		articles[i] = db.Article{
			Title:   fmt.Sprintf("Article %d", i+1),
			Link:    fmt.Sprintf("https://example.com/%d", i+1),
			Summary: "short",
		}
	}

	got, err := SummarizeWithAPI("user", "today", articles, "Japanese", "")
	if err != nil {
		t.Fatalf("SummarizeWithAPI: %v", err)
	}
	wantCalls := (MaxSummaryArticles + MaxArticlesPerAttempt - 1) / MaxArticlesPerAttempt
	if calls != wantCalls {
		t.Fatalf("api calls = %d, want %d", calls, wantCalls)
	}
	if !strings.HasPrefix(got, "## 本日のダイジェスト\n") {
		t.Fatalf("expected digest at top, got:\n%s", got)
	}
	if strings.Contains(got, "Today's Signal") {
		t.Fatalf("expected non-article section to be removed, got:\n%s", got)
	}
	if !strings.Contains(got, "21. [Article 21](https://example.com/21)") {
		t.Fatalf("expected second API chunk to be renumbered globally, got:\n%s", got)
	}
	if strings.Contains(got, "Article 51") {
		t.Fatalf("expected articles beyond summary limit to be omitted, got:\n%s", got)
	}
	if !strings.Contains(got, "50. [Article 50](https://example.com/50)") {
		t.Fatalf("expected article 50 to be included, got:\n%s", got)
	}
}

func TestSummarizeWithAPIReportsProgress(t *testing.T) {
	orig := apiSummary
	defer func() { apiSummary = orig }()

	apiSummary = func(_ string, articles []api.SummaryArticle, _ string, _ string) (string, error) {
		var b strings.Builder
		for i, a := range articles {
			if i > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(fmt.Sprintf("%d. [%s](%s)\n", i+1, a.Title, a.URL))
			b.WriteString("  - 要約\n")
			b.WriteString("    - ポイント1\n")
			b.WriteString("    - ポイント2\n")
		}
		return b.String(), nil
	}

	articles := make([]db.Article, MaxArticlesPerAttempt+1)
	for i := range articles {
		articles[i] = db.Article{
			Title:   fmt.Sprintf("Article %d", i+1),
			Link:    fmt.Sprintf("https://example.com/%d", i+1),
			Summary: "short",
		}
	}

	var events []ProgressEvent
	_, err := SummarizeWithAPIProgress("user", "today", articles, "Japanese", "", func(ev ProgressEvent) {
		events = append(events, ev)
	})
	if err != nil {
		t.Fatalf("SummarizeWithAPIProgress: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("events = %d, want 4: %#v", len(events), events)
	}
	if events[0].Phase != "start" || events[0].ChunkStart != 1 || events[0].ChunkEnd != MaxArticlesPerAttempt || events[0].TotalArticles != len(articles) {
		t.Fatalf("unexpected first event: %#v", events[0])
	}
	if events[2].Phase != "start" || events[2].ChunkStart != MaxArticlesPerAttempt+1 || events[2].ChunkEnd != MaxArticlesPerAttempt+1 {
		t.Fatalf("unexpected second chunk event: %#v", events[2])
	}
}

func TestSummarizeWithAPIDoesNotRetryPartialChunks(t *testing.T) {
	orig := apiSummary
	defer func() { apiSummary = orig }()

	calls := 0
	apiSummary = func(_ string, articles []api.SummaryArticle, _ string, _ string) (string, error) {
		calls++
		var b strings.Builder
		limit := min(3, len(articles))
		for i := 0; i < limit; i++ {
			if i > 0 {
				b.WriteString("\n\n")
			}
			b.WriteString(fmt.Sprintf("%d. [%s](%s)\n", i+1, articles[i].Title, articles[i].URL))
			b.WriteString("  - 要約\n")
			b.WriteString("    - ポイント1\n")
			b.WriteString("    - ポイント2\n")
		}
		return b.String(), nil
	}

	articles := make([]db.Article, MaxArticlesPerAttempt+5)
	for i := range articles {
		articles[i] = db.Article{
			Title:   fmt.Sprintf("Article %d", i+1),
			Link:    fmt.Sprintf("https://example.com/%d", i+1),
			Summary: "short",
		}
	}

	got, err := SummarizeWithAPI("user", "today", articles, "Japanese", "")
	if err != nil {
		t.Fatalf("SummarizeWithAPI: %v", err)
	}
	if calls != 2 {
		t.Fatalf("api calls = %d, want 2", calls)
	}
	if !strings.Contains(got, "20. [Article 20](https://example.com/20)") || !strings.Contains(got, "21. [Article 21](https://example.com/21)") {
		t.Fatalf("expected processing to advance to the next chunk, got:\n%s", got)
	}
	if !strings.Contains(got, "AI 出力に含まれなかったため自動補完しました。") {
		t.Fatalf("expected missing articles to be completed locally, got:\n%s", got)
	}
}
