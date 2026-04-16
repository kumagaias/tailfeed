// Package feed handles RSS/Atom feed polling.
package feed

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	gofeed "github.com/mmcdole/gofeed"

	"github.com/kumagaias/tailfeed/internal/db"
)

// NewArticleMsg is sent on the articles channel when a new article is saved.
type NewArticleMsg db.Article

// Poller fetches feeds in the background and emits new articles.
type Poller struct {
	db       *db.DB
	parser   *gofeed.Parser
	articles chan NewArticleMsg
	mu       sync.Mutex
	timers   map[int64]*time.Timer
}

// New creates a Poller. Call Start to begin polling.
func New(database *db.DB) *Poller {
	return &Poller{
		db:       database,
		parser:   gofeed.NewParser(),
		articles: make(chan NewArticleMsg, 64),
		timers:   make(map[int64]*time.Timer),
	}
}

// Articles returns the channel on which new articles are published.
func (p *Poller) Articles() <-chan NewArticleMsg {
	return p.articles
}

// Start begins polling all registered feeds and watches for new ones.
// It blocks until ctx is cancelled.
func (p *Poller) Start(ctx context.Context) {
	// initial fetch
	p.refreshAll(ctx)

	// re-schedule based on per-feed interval
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.refreshAll(ctx)
		}
	}
}

// PollFeed immediately polls a single feed by its DB record.
func (p *Poller) PollFeed(ctx context.Context, f db.Feed) {
	p.fetchFeed(ctx, f)
}

// PollAll immediately polls all registered feeds regardless of interval.
func (p *Poller) PollAll(ctx context.Context) {
	feeds, err := p.db.ListFeeds(nil)
	if err != nil {
		return
	}
	var wg sync.WaitGroup
	for _, f := range feeds {
		wg.Add(1)
		go func(feed db.Feed) {
			defer wg.Done()
			p.fetchFeed(ctx, feed)
		}(f)
	}
	wg.Wait()
}

func (p *Poller) refreshAll(ctx context.Context) {
	feeds, err := p.db.ListFeeds(nil)
	if err != nil {
		slog.Error("list feeds", "err", err)
		return
	}
	now := time.Now()
	var wg sync.WaitGroup
	for _, f := range feeds {
		if !p.isDue(f, now) {
			continue
		}
		wg.Add(1)
		go func(feed db.Feed) {
			defer wg.Done()
			p.fetchFeed(ctx, feed)
		}(f)
	}
	wg.Wait()
}

func (p *Poller) isDue(f db.Feed, now time.Time) bool {
	if f.LastFetchedAt == nil {
		return true
	}
	return now.After(f.LastFetchedAt.Add(time.Duration(f.FetchIntervalSeconds) * time.Second))
}

func (p *Poller) fetchFeed(ctx context.Context, f db.Feed) {
	if !strings.HasPrefix(f.URL, "http://") && !strings.HasPrefix(f.URL, "https://") {
		return
	}
	feed, err := p.parser.ParseURLWithContext(f.URL, ctx)
	if err != nil {
		slog.Warn("fetch feed", "url", f.URL, "err", err)
		return
	}

	// Update metadata (title, site URL)
	siteURL := ""
	if feed.Link != "" {
		siteURL = feed.Link
	}
	if err := p.db.UpdateFeedMeta(f.ID, feed.Title, siteURL); err != nil {
		slog.Warn("update feed meta", "id", f.ID, "err", err)
	}

	for _, item := range feed.Items {
		a := articleFromItem(f.ID, item)
		saved, err := p.db.SaveArticle(a)
		if err != nil {
			slog.Warn("save article", "err", err)
			continue
		}
		if saved {
			a.FeedTitle = feed.Title
			select {
			case p.articles <- NewArticleMsg(*a):
			default:
				// channel full — drop (UI will reload from DB)
			}
		}
	}
}

func articleFromItem(feedID int64, item *gofeed.Item) *db.Article {
	a := &db.Article{
		FeedID:  feedID,
		GUID:    item.GUID,
		Title:   item.Title,
		Link:    item.Link,
		Summary: item.Description,
	}
	if a.GUID == "" {
		a.GUID = item.Link
	}
	if item.PublishedParsed != nil {
		t := *item.PublishedParsed
		a.PublishedAt = &t
	} else if item.UpdatedParsed != nil {
		t := *item.UpdatedParsed
		a.PublishedAt = &t
	}
	return a
}
