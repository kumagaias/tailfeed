package db_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/kumagaias/tailfeed/internal/db"
	_ "modernc.org/sqlite"
)

// openTestDB creates a file-based temp SQLite DB and runs migrations.
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

// ── Group ─────────────────────────────────────────────────────────────────────

func TestGroupCRUD(t *testing.T) {
	d := openTestDB(t)

	g, err := d.CreateGroup("tech")
	if err != nil {
		t.Fatalf("CreateGroup: %v", err)
	}
	if g.Name != "tech" {
		t.Errorf("expected name=tech, got %q", g.Name)
	}

	groups, err := d.ListGroups()
	if err != nil {
		t.Fatalf("ListGroups: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}

	// duplicate name must fail
	if _, err := d.CreateGroup("tech"); err == nil {
		t.Error("expected error for duplicate group name")
	}

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

func TestGetGroupByName(t *testing.T) {
	d := openTestDB(t)
	_, _ = d.CreateGroup("sec")

	g, err := d.GetGroupByName("sec")
	if err != nil {
		t.Fatalf("GetGroupByName: %v", err)
	}
	if g.Name != "sec" {
		t.Errorf("expected name=sec, got %q", g.Name)
	}

	if _, err := d.GetGroupByName("nonexistent"); err == nil {
		t.Error("expected ErrGroupNotFound for missing group")
	}
}

// ── Feed ──────────────────────────────────────────────────────────────────────

func TestFeedCRUD(t *testing.T) {
	d := openTestDB(t)
	g, _ := d.CreateGroup("news")

	f, err := d.AddFeed("https://example.com/feed.rss", nil)
	if err != nil {
		t.Fatalf("AddFeed (ungrouped): %v", err)
	}
	if f.URL != "https://example.com/feed.rss" {
		t.Errorf("unexpected URL: %s", f.URL)
	}
	if f.GroupID != nil {
		t.Errorf("expected nil group, got %v", f.GroupID)
	}

	f2, err := d.AddFeed("https://news.example.com/rss", &g.ID)
	if err != nil {
		t.Fatalf("AddFeed (grouped): %v", err)
	}
	if f2.GroupID == nil || *f2.GroupID != g.ID {
		t.Errorf("expected group %d, got %v", g.ID, f2.GroupID)
	}

	// duplicate URL must fail
	if _, err := d.AddFeed("https://example.com/feed.rss", nil); err == nil {
		t.Error("expected ErrFeedAlreadyExists, got nil")
	}

	if err := d.RemoveFeed("https://example.com/feed.rss"); err != nil {
		t.Fatalf("RemoveFeed: %v", err)
	}
	feeds, _ := d.ListFeeds(nil)
	if len(feeds) != 1 {
		t.Errorf("expected 1 feed after remove, got %d", len(feeds))
	}
}

func TestFeedListFilterByGroup(t *testing.T) {
	d := openTestDB(t)
	g1, _ := d.CreateGroup("a")
	g2, _ := d.CreateGroup("b")

	_, _ = d.AddFeed("https://feed1.com/rss", &g1.ID)
	_, _ = d.AddFeed("https://feed2.com/rss", &g1.ID)
	_, _ = d.AddFeed("https://feed3.com/rss", &g2.ID)

	all, _ := d.ListFeeds(nil)
	if len(all) != 3 {
		t.Errorf("expected 3 total feeds, got %d", len(all))
	}
	inG1, _ := d.ListFeeds(&g1.ID)
	if len(inG1) != 2 {
		t.Errorf("expected 2 feeds in group a, got %d", len(inG1))
	}
}

func TestMaxFeedsPerGroup(t *testing.T) {
	d := openTestDB(t)
	g, _ := d.CreateGroup("big")
	for i := range db.MaxFeedsPerGroup {
		url := "https://feed" + itoa(i) + ".example.com/rss"
		if _, err := d.AddFeed(url, &g.ID); err != nil {
			t.Fatalf("AddFeed %d: %v", i, err)
		}
	}
	if _, err := d.AddFeed("https://overflow.example.com/rss", &g.ID); err == nil {
		t.Error("expected ErrMaxFeedsPerGroup, got nil")
	}
}

func TestUpdateFeedMeta(t *testing.T) {
	d := openTestDB(t)
	f, _ := d.AddFeed("https://example.com/rss", nil)

	if err := d.UpdateFeedMeta(f.ID, "Example Blog", "https://example.com"); err != nil {
		t.Fatalf("UpdateFeedMeta: %v", err)
	}
	feeds, _ := d.ListFeeds(nil)
	if feeds[0].Title != "Example Blog" {
		t.Errorf("expected title=Example Blog, got %q", feeds[0].Title)
	}
	if feeds[0].LastFetchedAt == nil {
		t.Error("expected LastFetchedAt to be set after UpdateFeedMeta")
	}
}

// ── Article ───────────────────────────────────────────────────────────────────

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

	// duplicate must be ignored
	saved, err = d.SaveArticle(a)
	if err != nil {
		t.Fatalf("SaveArticle duplicate: %v", err)
	}
	if saved {
		t.Error("expected saved=false for duplicate")
	}

	articles, err := d.ListArticles(nil, 50, 0)
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

func TestArticleMarkRead(t *testing.T) {
	d := openTestDB(t)
	f, _ := d.AddFeed("https://example.com/rss", nil)
	a := &db.Article{FeedID: f.ID, GUID: "g1", Title: "T1", Link: "https://example.com/1"}
	_, _ = d.SaveArticle(a)

	articles, _ := d.ListArticles(nil, 10, 0)
	if articles[0].IsRead {
		t.Fatal("article should be unread initially")
	}

	if err := d.MarkRead(articles[0].ID); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}
	articles, _ = d.ListArticles(nil, 10, 0)
	if !articles[0].IsRead {
		t.Error("article should be read after MarkRead")
	}
}

func TestArticleListFilterByGroup(t *testing.T) {
	d := openTestDB(t)
	g, _ := d.CreateGroup("tech")
	f1, _ := d.AddFeed("https://feed1.com/rss", &g.ID)
	f2, _ := d.AddFeed("https://feed2.com/rss", nil)

	_, _ = d.SaveArticle(&db.Article{FeedID: f1.ID, GUID: "a1", Title: "In group"})
	_, _ = d.SaveArticle(&db.Article{FeedID: f2.ID, GUID: "a2", Title: "Not in group"})

	all, _ := d.ListArticles(nil, 50, 0)
	if len(all) != 2 {
		t.Errorf("expected 2 total articles, got %d", len(all))
	}
	inGroup, _ := d.ListArticles(&g.ID, 50)
	if len(inGroup) != 1 {
		t.Errorf("expected 1 article in group, got %d", len(inGroup))
	}
	if inGroup[0].Title != "In group" {
		t.Errorf("unexpected title: %s", inGroup[0].Title)
	}
}

func TestArticleOrderedOldestFirst(t *testing.T) {
	d := openTestDB(t)
	f, _ := d.AddFeed("https://example.com/rss", nil)

	for i := range 5 {
		_, _ = d.SaveArticle(&db.Article{
			FeedID: f.ID,
			GUID:   "g" + itoa(i),
			Title:  "Article " + itoa(i),
		})
	}
	articles, _ := d.ListArticles(nil, 50, 0)
	for i, a := range articles {
		expected := "Article " + itoa(i)
		if a.Title != expected {
			t.Errorf("position %d: expected %q, got %q", i, expected, a.Title)
		}
	}
}

func itoa(n int) string {
	return string(rune('0' + n))
}
