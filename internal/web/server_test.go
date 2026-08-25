package web

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/kumagaias/tailfeed/internal/db"
	_ "modernc.org/sqlite"
)

func openWebTestDB(t *testing.T) *db.DB {
	t.Helper()
	sqlDB, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "web.db")+"?_foreign_keys=on")
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

func TestFeedHandlersAddListAndRemove(t *testing.T) {
	database := openWebTestDB(t)
	server := New(database)

	body, _ := json.Marshal(addFeedRequest{URL: "https://example.com/feed.xml"})
	request := httptest.NewRequest(http.MethodPost, "/api/feeds", bytes.NewReader(body))
	response := httptest.NewRecorder()
	server.handleFeeds(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("add feed status = %d, body = %q", response.Code, response.Body.String())
	}

	feeds, err := database.ListFeeds(nil)
	if err != nil || len(feeds) != 1 {
		t.Fatalf("feeds after add = %#v, err = %v", feeds, err)
	}

	request = httptest.NewRequest(http.MethodDelete, "/api/feeds/"+strconv.FormatInt(feeds[0].ID, 10), nil)
	response = httptest.NewRecorder()
	server.handleFeed(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("remove feed status = %d, body = %q", response.Code, response.Body.String())
	}
}

func TestFirstImageURL(t *testing.T) {
	got := firstImageURL(`<p>hello</p><img alt="x" src="/img/card.jpg">`, "https://example.com/posts/1")
	want := "https://example.com/img/card.jpg"
	if got != want {
		t.Fatalf("firstImageURL() = %q, want %q", got, want)
	}
}

func TestFirstImageURLSkipsDataURI(t *testing.T) {
	got := firstImageURL(`<img src="data:image/png;base64,xxx">`, "https://example.com/posts/1")
	if got != "" {
		t.Fatalf("firstImageURL() = %q, want empty", got)
	}
}

func TestPaginateArticlesReturnsNewestPageInDisplayOrder(t *testing.T) {
	articles := make([]db.Article, 120)
	for i := range articles {
		articles[i].ID = int64(i + 1)
	}

	page, hasMore, err := paginateArticles(articles, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if !hasMore {
		t.Fatal("expected more articles")
	}
	if len(page) != 50 || page[0].ID != 71 || page[49].ID != 120 {
		t.Fatalf("unexpected first page: len=%d first=%d last=%d", len(page), page[0].ID, page[len(page)-1].ID)
	}

	page, hasMore, err = paginateArticles(articles, 50, 50)
	if err != nil {
		t.Fatal(err)
	}
	if !hasMore {
		t.Fatal("expected a third page")
	}
	if len(page) != 50 || page[0].ID != 21 || page[49].ID != 70 {
		t.Fatalf("unexpected second page: len=%d first=%d last=%d", len(page), page[0].ID, page[len(page)-1].ID)
	}
}
