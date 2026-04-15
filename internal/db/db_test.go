package db_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/kumagaias/tailfeed/internal/db"
	_ "modernc.org/sqlite"
)

// openTestDB creates an in-memory SQLite DB for tests.
func openTestDB(t *testing.T) *db.DB {
	t.Helper()
	// Use a file-based temp DB so the embedded migration SQL runs properly.
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

func TestGroupCRUD(t *testing.T) {
	d := openTestDB(t)

	// Create
	g, err := d.CreateGroup("tech")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if g.Name != "tech" {
		t.Errorf("expected name=tech, got %q", g.Name)
	}

	// List
	groups, err := d.ListGroups()
	if err != nil {
		t.Fatalf("ListGroups: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}

	// Duplicate name should fail
	if _, err := d.CreateGroup("tech"); err == nil {
		t.Error("expected error for duplicate group name")
	}

	// Delete
	if err := d.DeleteGroup(g.ID); err != nil {
		t.Fatalf("DeleteGroup: %v", err)
	}
	groups, _ = d.ListGroups()
	if len(groups) != 0 {
		t.Errorf("expected 0 groups after delete, got %d", len(groups))
	}
}

func TestMaxGroups(t *testing.T) {
	d := openTestDB(t)
	for i := range db.MaxGroups {
		name := "group" + string(rune('a'+i))
		if _, err := d.CreateGroup(name); err != nil {
			t.Fatalf("CreateGroup %d: %v", i, err)
		}
	}
	if _, err := d.CreateGroup("overflow"); err == nil {
		t.Error("expected ErrMaxGroups, got nil")
	}
}

func TestFeedCRUD(t *testing.T) {
	d := openTestDB(t)
	g, _ := d.CreateGroup("news")

	// Add feed without group
	f, err := d.AddFeed("https://example.com/feed.rss", nil)
	if err != nil {
		t.Fatalf("AddFeed: %v", err)
	}
	if f.URL != "https://example.com/feed.rss" {
		t.Errorf("unexpected URL: %s", f.URL)
	}

	// Add feed with group
	f2, err := d.AddFeed("https://news.example.com/rss", &g.ID)
	if err != nil {
		t.Fatalf("AddFeed with group: %v", err)
	}
	if f2.GroupID == nil || *f2.GroupID != g.ID {
		t.Errorf("expected group %d, got %v", g.ID, f2.GroupID)
	}

	// Duplicate URL should fail
	if _, err := d.AddFeed("https://example.com/feed.rss", nil); err == nil {
		t.Error("expected ErrFeedAlreadyExists, got nil")
	}

	// Remove
	if err := d.RemoveFeed("https://example.com/feed.rss"); err != nil {
		t.Fatalf("RemoveFeed: %v", err)
	}
	feeds, _ := d.ListFeeds(nil)
	if len(feeds) != 1 {
		t.Errorf("expected 1 feed after remove, got %d", len(feeds))
	}
}

func TestArticleSaveAndList(t *testing.T) {
	d := openTestDB(t)
	f, _ := d.AddFeed("https://example.com/feed.rss", nil)

	a := &db.Article{
		FeedID:  f.ID,
		GUID:    "item-1",
		Title:   "Hello World",
		Link:    "https://example.com/hello",
		Summary: "A test article",
	}
	saved, err := d.SaveArticle(a)
	if err != nil {
		t.Fatalf("SaveArticle: %v", err)
	}
	if !saved {
		t.Error("expected saved=true for new article")
	}

	// Duplicate should be ignored
	saved, err = d.SaveArticle(a)
	if err != nil {
		t.Fatalf("SaveArticle duplicate: %v", err)
	}
	if saved {
		t.Error("expected saved=false for duplicate")
	}

	articles, err := d.ListArticles(nil, 50)
	if err != nil {
		t.Fatalf("ListArticles: %v", err)
	}
	if len(articles) != 1 {
		t.Fatalf("expected 1 article, got %d", len(articles))
	}
	if articles[0].Title != "Hello World" {
		t.Errorf("unexpected title: %s", articles[0].Title)
	}
}
