package db

import (
	"database/sql"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"
)

// Article represents a fetched RSS item.
type Article struct {
	ID          int64
	FeedID      int64
	FeedTitle   string // from joined feeds row
	GUID        string
	Title       string
	Link        string
	Summary     string
	PublishedAt *time.Time
	IsRead      bool
	IsStocked   bool
	CreatedAt   time.Time
}

// SaveArticle inserts an article, silently ignoring duplicates.
func (d *DB) SaveArticle(a *Article) (saved bool, err error) {
	if key := articleDedupeKey(*a); key != "" {
		rows, err := d.Query(`SELECT feed_id, guid, COALESCE(link,''), title, published_at, created_at FROM articles`)
		if err != nil {
			return false, fmt.Errorf("check duplicate article: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var existing Article
			var pub sql.NullTime
			if err := rows.Scan(&existing.FeedID, &existing.GUID, &existing.Link, &existing.Title, &pub, &existing.CreatedAt); err != nil {
				return false, fmt.Errorf("check duplicate article: %w", err)
			}
			if pub.Valid {
				existing.PublishedAt = &pub.Time
			}
			if articleDedupeKey(existing) == key {
				return false, nil
			}
		}
		if err := rows.Err(); err != nil {
			return false, fmt.Errorf("check duplicate article: %w", err)
		}
	}

	res, err := d.Exec(
		`INSERT OR IGNORE INTO articles (feed_id, guid, title, link, summary, published_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		a.FeedID, a.GUID, a.Title, a.Link, a.Summary, a.PublishedAt,
	)
	if err != nil {
		return false, fmt.Errorf("save article: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

const articleSelectQ = `
	SELECT a.id, a.feed_id, COALESCE(f.title, f.url),
	       a.guid, a.title, COALESCE(a.link,''), COALESCE(a.summary,''),
	       a.published_at, a.is_read, a.is_stocked, a.created_at
	FROM articles a
	JOIN feeds f ON f.id = a.feed_id
	%s
	ORDER BY COALESCE(a.published_at, a.created_at) DESC
	LIMIT ? OFFSET ?`

const articleSelectDescQ = `
	SELECT a.id, a.feed_id, COALESCE(f.title, f.url),
	       a.guid, a.title, COALESCE(a.link,''), COALESCE(a.summary,''),
	       a.published_at, a.is_read, a.is_stocked, a.created_at
	FROM articles a
	JOIN feeds f ON f.id = a.feed_id
	%s
	ORDER BY COALESCE(a.published_at, a.created_at) DESC
	LIMIT ?`

// ListArticles returns articles ordered newest-first.
func (d *DB) ListArticles(groupID *int64, limit, offset int) ([]Article, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if groupID == nil {
		rows, err = d.Query(fmt.Sprintf(articleSelectQ, ""), limit, offset)
	} else {
		rows, err = d.Query(fmt.Sprintf(articleSelectQ, "WHERE f.group_id = ?"), *groupID, limit, offset)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	articles, err := scanArticles(rows)
	if err != nil {
		return nil, err
	}
	return dedupeArticles(articles), nil
}

// ListRecentArticles returns the N most recent articles in newest-first order.
// groupID=nil means all groups.
func (d *DB) ListRecentArticles(groupID *int64, limit int) ([]Article, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if groupID == nil {
		rows, err = d.Query(fmt.Sprintf(articleSelectDescQ, ""), limit)
	} else {
		rows, err = d.Query(fmt.Sprintf(articleSelectDescQ, "WHERE f.group_id = ?"), *groupID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	articles, err := scanArticles(rows)
	if err != nil {
		return nil, err
	}
	return dedupeArticles(articles), nil
}

// ListTodayArticles returns all articles published or created in the last 24 hours.
func (d *DB) ListTodayArticles() ([]Article, error) {
	return d.listArticlesWithinDurationAt(time.Now(), 24*time.Hour)
}

// ListYesterdayArticles returns all articles published or created yesterday (local time).
func (d *DB) ListYesterdayArticles() ([]Article, error) {
	return d.listArticlesByDateRange(-1, 1)
}

// ListWeekArticles returns all articles published or created in the last 7 days (local time).
func (d *DB) ListWeekArticles() ([]Article, error) {
	return d.listArticlesByDateRange(-6, 7)
}

func (d *DB) listArticlesByDateRange(daysOffset, duration int) ([]Article, error) {
	return d.listArticlesByDateRangeAt(time.Now(), daysOffset, duration)
}

func (d *DB) listArticlesByDateRangeAt(now time.Time, daysOffset, duration int) ([]Article, error) {
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, daysOffset)
	end := start.AddDate(0, 0, duration)
	return d.listArticlesBetween(start, end)
}

func (d *DB) listArticlesWithinDurationAt(now time.Time, duration time.Duration) ([]Article, error) {
	return d.listArticlesBetween(now.Add(-duration), now)
}

func (d *DB) listArticlesBetween(start, end time.Time) ([]Article, error) {
	rows, err := d.Query(`
		SELECT a.id, a.feed_id, COALESCE(f.title, f.url),
		       a.guid, a.title, COALESCE(a.link,''), COALESCE(a.summary,''),
		       a.published_at, a.is_read, a.is_stocked, a.created_at
		FROM articles a
		JOIN feeds f ON f.id = a.feed_id
		ORDER BY COALESCE(a.published_at, a.created_at) DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	articles, err := scanArticles(rows)
	if err != nil {
		return nil, err
	}

	filtered := articles[:0]
	for _, a := range articles {
		t := articleTime(a)
		if !t.Before(start) && t.Before(end) {
			filtered = append(filtered, a)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return articleTime(filtered[i]).After(articleTime(filtered[j]))
	})
	return dedupeArticles(filtered), nil
}

func articleTime(a Article) time.Time {
	if a.PublishedAt != nil {
		return *a.PublishedAt
	}
	return a.CreatedAt
}

func dedupeArticles(articles []Article) []Article {
	seen := make(map[string]bool, len(articles))
	out := articles[:0]
	for _, a := range articles {
		key := articleDedupeKey(a)
		if key != "" {
			if seen[key] {
				continue
			}
			seen[key] = true
		}
		out = append(out, a)
	}
	return out
}

func articleDedupeKey(a Article) string {
	if link := normalizedArticleLink(a.Link); link != "" {
		return "link:" + link
	}
	if guid := normalizedArticleLink(a.GUID); guid != "" {
		return "link:" + guid
	}
	if guid := strings.TrimSpace(a.GUID); guid != "" {
		return "guid:" + guid
	}
	return ""
}

func normalizedArticleLink(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	u.Fragment = ""

	q := u.Query()
	for key := range q {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "utm_") || lower == "fbclid" || lower == "gclid" || lower == "yclid" || lower == "mc_cid" || lower == "mc_eid" {
			q.Del(key)
		}
	}
	u.RawQuery = q.Encode()
	if u.Path != "/" {
		u.Path = strings.TrimRight(u.Path, "/")
	}
	return u.String()
}

// ListStockedArticles returns articles marked as stocked (favourites).
func (d *DB) ListStockedArticles(limit, offset int) ([]Article, error) {
	rows, err := d.Query(fmt.Sprintf(articleSelectQ, "WHERE a.is_stocked = 1"), limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	articles, err := scanArticles(rows)
	if err != nil {
		return nil, err
	}
	return dedupeArticles(articles), nil
}

// MarkRead marks an article as read.
func (d *DB) MarkRead(id int64) error {
	_, err := d.Exec(`UPDATE articles SET is_read = 1 WHERE id = ?`, id)
	return err
}

// ToggleStock flips the is_stocked flag for an article.
func (d *DB) ToggleStock(id int64) error {
	_, err := d.Exec(`UPDATE articles SET is_stocked = 1 - is_stocked WHERE id = ?`, id)
	return err
}

func scanArticles(rows *sql.Rows) ([]Article, error) {
	var articles []Article
	for rows.Next() {
		var a Article
		var pub sql.NullTime
		var isRead, isStocked int
		if err := rows.Scan(
			&a.ID, &a.FeedID, &a.FeedTitle,
			&a.GUID, &a.Title, &a.Link, &a.Summary,
			&pub, &isRead, &isStocked, &a.CreatedAt,
		); err != nil {
			return nil, err
		}
		if pub.Valid {
			a.PublishedAt = &pub.Time
		}
		a.IsRead = isRead == 1
		a.IsStocked = isStocked == 1
		articles = append(articles, a)
	}
	return articles, rows.Err()
}
