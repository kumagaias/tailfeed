package web

import (
	"testing"

	"github.com/kumagaias/tailfeed/internal/db"
)

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
