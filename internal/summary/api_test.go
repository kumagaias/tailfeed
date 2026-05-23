package summary

import (
	"fmt"
	"strings"
	"testing"

	"github.com/kumagaias/tailfeed/internal/api"
	"github.com/kumagaias/tailfeed/internal/db"
)

func TestSummarizeWithAPICapsCallsAtThree(t *testing.T) {
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

	articles := make([]db.Article, MaxArticlesPerAttempt*MaxAPIAttempts+2)
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
	if calls != MaxAPIAttempts {
		t.Fatalf("api calls = %d, want %d", calls, MaxAPIAttempts)
	}
	if strings.Contains(got, "Today's Signal") {
		t.Fatalf("expected non-article section to be removed, got:\n%s", got)
	}
	if !strings.Contains(got, "9. [Article 9](https://example.com/9)") {
		t.Fatalf("expected second API chunk to be renumbered globally, got:\n%s", got)
	}
	if !strings.Contains(got, "25. [Article 25](https://example.com/25)") {
		t.Fatalf("expected local fallback for articles beyond three API calls, got:\n%s", got)
	}
	if !strings.Contains(got, "API 呼び出し上限により自動補完しました。") {
		t.Fatalf("expected API fallback reason, got:\n%s", got)
	}
}
