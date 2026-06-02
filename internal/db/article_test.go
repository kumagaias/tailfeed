package db

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func openArticleTestDB(t *testing.T) *DB {
	t.Helper()
	dir := t.TempDir()
	dsn := filepath.Join(dir, "test.db") + "?_foreign_keys=on"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	d := WrapDB(sqlDB)
	if err := d.MigrateForTest(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return d
}

func TestListTodayArticlesUsesLocalDayBoundaries(t *testing.T) {
	d := openArticleTestDB(t)
	f, err := d.AddFeed("https://example.com/rss", nil)
	if err != nil {
		t.Fatalf("AddFeed: %v", err)
	}
	loc, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	now := time.Date(2026, 5, 21, 9, 0, 0, 0, loc)

	cases := []struct {
		guid      string
		title     string
		published time.Time
	}{
		{
			guid:      "before-local-day",
			title:     "Before local day",
			published: time.Date(2026, 5, 20, 14, 59, 59, 0, time.UTC),
		},
		{
			guid:      "utc-previous-day-local-today",
			title:     "UTC previous day, local today",
			published: time.Date(2026, 5, 20, 16, 0, 0, 0, time.UTC),
		},
		{
			guid:      "local-today",
			title:     "Local today",
			published: time.Date(2026, 5, 21, 12, 0, 0, 0, loc),
		},
		{
			guid:      "after-local-day",
			title:     "After local day",
			published: time.Date(2026, 5, 21, 15, 0, 0, 0, time.UTC),
		},
	}

	for _, tc := range cases {
		published := tc.published
		_, err := d.SaveArticle(&Article{
			FeedID:      f.ID,
			GUID:        tc.guid,
			Title:       tc.title,
			Link:        "https://example.com/" + tc.guid,
			PublishedAt: &published,
		})
		if err != nil {
			t.Fatalf("SaveArticle %s: %v", tc.guid, err)
		}
	}
	articles, err := d.listArticlesByDateRangeAt(now, 0, 1)
	if err != nil {
		t.Fatalf("listArticlesByDateRangeAt: %v", err)
	}
	if len(articles) != 2 {
		t.Fatalf("expected 2 articles for local today, got %d: %#v", len(articles), articles)
	}

	got := map[string]bool{}
	for _, a := range articles {
		got[a.GUID] = true
	}
	for _, guid := range []string{"utc-previous-day-local-today", "local-today"} {
		if !got[guid] {
			t.Fatalf("expected %q in local today results, got %#v", guid, got)
		}
	}
}

func TestSaveArticleDedupesNormalizedLinks(t *testing.T) {
	d := openArticleTestDB(t)
	f1, err := d.AddFeed("https://example.com/rss", nil)
	if err != nil {
		t.Fatalf("AddFeed f1: %v", err)
	}
	f2, err := d.AddFeed("https://mirror.example.com/rss", nil)
	if err != nil {
		t.Fatalf("AddFeed f2: %v", err)
	}

	saved, err := d.SaveArticle(&Article{
		FeedID: f1.ID,
		GUID:   "first",
		Title:  "Article",
		Link:   "https://example.com/post?utm_source=rss#comments",
	})
	if err != nil {
		t.Fatalf("SaveArticle first: %v", err)
	}
	if !saved {
		t.Fatal("expected first article to be saved")
	}

	saved, err = d.SaveArticle(&Article{
		FeedID: f2.ID,
		GUID:   "second",
		Title:  "Article duplicate",
		Link:   "https://EXAMPLE.com/post/",
	})
	if err != nil {
		t.Fatalf("SaveArticle duplicate: %v", err)
	}
	if saved {
		t.Fatal("expected normalized duplicate link to be ignored")
	}
}

func TestListTodayArticlesDedupesExistingNormalizedLinks(t *testing.T) {
	d := openArticleTestDB(t)
	f, err := d.AddFeed("https://example.com/rss", nil)
	if err != nil {
		t.Fatalf("AddFeed: %v", err)
	}
	now := time.Date(2026, 5, 21, 9, 0, 0, 0, time.Local)
	published := now.Add(-time.Hour)

	_, err = d.Exec(
		`INSERT INTO articles (feed_id, guid, title, link, published_at) VALUES (?, ?, ?, ?, ?)`,
		f.ID, "first", "Article", "https://example.com/post?utm_medium=feed", published,
	)
	if err != nil {
		t.Fatalf("insert first: %v", err)
	}
	_, err = d.Exec(
		`INSERT INTO articles (feed_id, guid, title, link, published_at) VALUES (?, ?, ?, ?, ?)`,
		f.ID, "second", "Article duplicate", "https://example.com/post#read", published,
	)
	if err != nil {
		t.Fatalf("insert duplicate: %v", err)
	}

	articles, err := d.listArticlesByDateRangeAt(now, 0, 1)
	if err != nil {
		t.Fatalf("listArticlesByDateRangeAt: %v", err)
	}
	if len(articles) != 1 {
		t.Fatalf("expected 1 deduped article, got %d: %#v", len(articles), articles)
	}
}
