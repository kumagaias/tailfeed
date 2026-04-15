package db

import (
	"database/sql"
	"fmt"
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

// SaveArticle inserts an article, silently ignoring duplicates (GUID collision).
func (d *DB) SaveArticle(a *Article) (saved bool, err error) {
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
	ORDER BY COALESCE(a.published_at, a.created_at) ASC
	LIMIT ?`

// ListArticles returns the most recent articles. groupID=nil means all groups.
func (d *DB) ListArticles(groupID *int64, limit int) ([]Article, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if groupID == nil {
		rows, err = d.Query(fmt.Sprintf(articleSelectQ, ""), limit)
	} else {
		rows, err = d.Query(fmt.Sprintf(articleSelectQ, "WHERE f.group_id = ?"), *groupID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanArticles(rows)
}

// ListStockedArticles returns articles marked as stocked (favourites).
func (d *DB) ListStockedArticles(limit int) ([]Article, error) {
	rows, err := d.Query(fmt.Sprintf(articleSelectQ, "WHERE a.is_stocked = 1"), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanArticles(rows)
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
