package summary

import (
	"fmt"
	"strings"

	"github.com/kumagaias/tailfeed/internal/api"
	"github.com/kumagaias/tailfeed/internal/db"
)

const MaxAPIAttempts = 5

var apiSummary = api.Summary

// SummarizeWithAPI summarizes articles through the tailfeed API in chunks so
// larger daily summaries do not exceed the API response timeout.
func SummarizeWithAPI(userKey, label string, articles []db.Article, language, theme string) (string, error) {
	return SummarizeWithAPIProgress(userKey, label, articles, language, theme, nil)
}

func SummarizeWithAPIProgress(userKey, label string, articles []db.Article, language, theme string, progress ProgressFunc) (string, error) {
	articles = LimitArticles(articles)
	language = strings.TrimSpace(language)
	if language == "" {
		language = "Japanese"
	}

	var out []string
	pending := append([]db.Article(nil), articles...)
	completed := 0
	for attempt := 1; attempt <= MaxAPIAttempts && len(pending) > 0; attempt++ {
		chunk, rest := nextChunk(pending, DefaultMaxContextRunes)
		if len(chunk) == 0 {
			chunk = pending[:1]
			rest = pending[1:]
		}
		notifySummaryProgress(progress, "start", attempt, MaxAPIAttempts, completed+1, len(chunk), len(articles))

		apiArticles := make([]api.SummaryArticle, len(chunk))
		for i, a := range chunk {
			apiArticles[i] = api.SummaryArticle{
				Title:   a.Title,
				URL:     a.Link,
				Summary: PlainText(a.Summary, 300),
			}
		}

		chunkTheme := ThemeWithLanguageInstruction(theme, language)
		chunkTheme += fmt.Sprintf(`
<chunk>
  <period>%s</period>
  <rule>This is one chunk of a larger summary. Include every provided article exactly once.</rule>
  <rule>Number article links in the order provided.</rule>
</chunk>`, xmlEscape(label))
		text, err := apiSummary(userKey, apiArticles, language, chunkTheme)
		if err != nil {
			return "", err
		}
		notifySummaryProgress(progress, "done", attempt, MaxAPIAttempts, completed+1, len(chunk), len(articles))
		normalized, chunkCompleted := completeArticleBlocks(text, chunk, completed+1)
		if chunkCompleted > 0 {
			out = append(out, normalized)
			if chunkCompleted > len(chunk) {
				chunkCompleted = len(chunk)
			}
			completed += chunkCompleted
			pending = append(append([]db.Article(nil), chunk[chunkCompleted:]...), rest...)
			continue
		}
		if trimmed := strings.TrimSpace(text); trimmed != "" {
			out = append(out, trimmed)
		}
		completed += len(chunk)
		pending = rest
	}
	if len(pending) > 0 {
		out = append(out, fallbackBlocksWithReason(pending, completed+1, "API 呼び出し上限により自動補完しました。"))
	}

	return prependDigest(strings.Join(out, "\n\n"), label, articles, language), nil
}
