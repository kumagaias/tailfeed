package tui

import (
	"database/sql"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/viewport"

	"github.com/kumagaias/tailfeed/internal/db"
	_ "modernc.org/sqlite"
)

func TestLoadOlderPageUsesNextDatabaseOffset(t *testing.T) {
	database := openArticleLoadingTestDB(t)
	feed, err := database.AddFeed("https://example.com/rss", nil)
	if err != nil {
		t.Fatalf("add feed: %v", err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < articlesLimit+articlesPageSize+1; i++ {
		_, err := database.Exec(
			`INSERT INTO articles (feed_id, guid, title, link, published_at) VALUES (?, ?, ?, ?, ?)`,
			feed.ID, "guid-"+strconv.Itoa(i), "Article "+strconv.Itoa(i),
			"https://example.com/article/"+strconv.Itoa(i), base.Add(time.Duration(i)*time.Minute),
		)
		if err != nil {
			t.Fatalf("insert article %d: %v", i, err)
		}
	}

	m := &Model{
		db:       database,
		tabs:     []groupTab{{name: "All"}},
		width:    40,
		height:   16,
		viewport: viewport.New(40, 12),
	}
	if err := m.reloadArticles(); err != nil {
		t.Fatalf("initial load: %v", err)
	}
	if len(m.articles) != articlesLimit || m.articlesOffset != articlesLimit || !m.articlesHasMore {
		t.Fatalf("initial page: len=%d offset=%d hasMore=%v", len(m.articles), m.articlesOffset, m.articlesHasMore)
	}

	gotModel, cmd := m.Update(loadOlderMsg{})
	if cmd == nil {
		t.Fatal("expected older-page command")
	}
	done, ok := cmd().(loadOlderDoneMsg)
	if !ok {
		t.Fatalf("command returned an unexpected message")
	}
	gotModel, _ = gotModel.(*Model).Update(done)
	got := gotModel.(*Model)

	if got.articlesOffset != articlesLimit+articlesPageSize {
		t.Fatalf("offset after older page = %d, want %d", got.articlesOffset, articlesLimit+articlesPageSize)
	}
	if len(got.articles) != articlesLimit+articlesPageSize || got.articles[0].Title != "Article 1" {
		t.Fatalf("articles after older page: len=%d first=%q", len(got.articles), got.articles[0].Title)
	}
	if !got.articlesHasMore {
		t.Fatal("expected the final older article to remain available")
	}
}

func openArticleLoadingTestDB(t *testing.T) *db.DB {
	t.Helper()
	sqlDB, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "tui.db")+"?_foreign_keys=on")
	if err != nil {
		t.Fatal(err)
	}
	database := db.WrapDB(sqlDB)
	if err := database.MigrateForTest(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}
