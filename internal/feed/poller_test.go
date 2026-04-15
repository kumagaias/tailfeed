package feed

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/kumagaias/tailfeed/internal/db"
	_ "modernc.org/sqlite"
)

func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	dir := t.TempDir()
	dsn := filepath.Join(dir, "test.db") + "?_foreign_keys=on"
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	d := db.WrapDB(sqlDB)
	if err := d.MigrateForTest(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return d
}

func TestArticleFromItem_GUID_fallback(t *testing.T) {
	item := &mockFeedItem{
		guid:  "",
		link:  "https://example.com/post",
		title: "Test",
	}
	a := articleFromMockItem(1, item)
	if a.GUID != "https://example.com/post" {
		t.Errorf("expected GUID to fall back to link, got %q", a.GUID)
	}
}

func TestArticleFromItem_Published(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	item := &mockFeedItem{
		guid:      "x",
		link:      "https://example.com",
		title:     "T",
		published: &now,
	}
	a := articleFromMockItem(1, item)
	if a.PublishedAt == nil {
		t.Fatal("expected PublishedAt to be set")
	}
	if !a.PublishedAt.Equal(now) {
		t.Errorf("expected %v, got %v", now, *a.PublishedAt)
	}
}

func TestIsDue(t *testing.T) {
	d := openTestDB(t)
	p := New(d)

	past := time.Now().Add(-20 * time.Minute)
	recent := time.Now().Add(-1 * time.Minute)

	feedDue := db.Feed{FetchIntervalSeconds: 900, LastFetchedAt: &past}
	feedNotDue := db.Feed{FetchIntervalSeconds: 900, LastFetchedAt: &recent}
	feedNeverFetched := db.Feed{FetchIntervalSeconds: 900, LastFetchedAt: nil}

	if !p.isDue(feedDue, time.Now()) {
		t.Error("feed last fetched 20m ago with 15m interval should be due")
	}
	if p.isDue(feedNotDue, time.Now()) {
		t.Error("feed last fetched 1m ago with 15m interval should not be due")
	}
	if !p.isDue(feedNeverFetched, time.Now()) {
		t.Error("feed never fetched should always be due")
	}
}

// ── minimal mock helpers (avoid real HTTP in tests) ──────────────────────────

type mockFeedItem struct {
	guid      string
	link      string
	title     string
	published *time.Time
}

func articleFromMockItem(feedID int64, item *mockFeedItem) *db.Article {
	a := &db.Article{
		FeedID: feedID,
		GUID:   item.guid,
		Title:  item.title,
		Link:   item.link,
	}
	if a.GUID == "" {
		a.GUID = item.link
	}
	if item.published != nil {
		t := *item.published
		a.PublishedAt = &t
	}
	return a
}
