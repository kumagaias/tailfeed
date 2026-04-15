package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

const MaxFeedsPerGroup = 100

// ErrFeedNotFound is returned when a feed does not exist.
var ErrFeedNotFound = errors.New("feed not found")

// ErrMaxFeedsPerGroup is returned when the per-group feed limit is reached.
var ErrMaxFeedsPerGroup = fmt.Errorf("maximum %d feeds per group reached", MaxFeedsPerGroup)

// ErrFeedAlreadyExists is returned when the feed URL is already registered.
var ErrFeedAlreadyExists = errors.New("feed already registered")

// Feed represents a subscribed RSS/Atom feed.
type Feed struct {
	ID                   int64
	GroupID              *int64
	URL                  string
	Title                string
	SiteURL              string
	LastFetchedAt        *time.Time
	FetchIntervalSeconds int
	CreatedAt            time.Time
}

// AddFeed registers a new feed. groupID may be nil (ungrouped).
func (d *DB) AddFeed(url string, groupID *int64) (*Feed, error) {
	if groupID != nil {
		n, err := d.countFeedsInGroup(*groupID)
		if err != nil {
			return nil, err
		}
		if n >= MaxFeedsPerGroup {
			return nil, ErrMaxFeedsPerGroup
		}
	}
	res, err := d.Exec(
		`INSERT INTO feeds (url, group_id) VALUES (?, ?)`, url, groupID,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrFeedAlreadyExists
		}
		return nil, fmt.Errorf("add feed: %w", err)
	}
	id, _ := res.LastInsertId()
	return &Feed{ID: id, URL: url, GroupID: groupID, FetchIntervalSeconds: 900}, nil
}

// RemoveFeed deletes a feed by URL.
func (d *DB) RemoveFeed(url string) error {
	res, err := d.Exec(`DELETE FROM feeds WHERE url = ?`, url)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrFeedNotFound
	}
	return nil
}

// ListFeeds returns feeds, optionally filtered by group. nil means all feeds.
func (d *DB) ListFeeds(groupID *int64) ([]Feed, error) {
	var (
		rows *sql.Rows
		err  error
	)
	const q = `SELECT id, group_id, url, COALESCE(title,''), COALESCE(site_url,''),
	             last_fetched_at, fetch_interval_seconds, created_at
	             FROM feeds %s ORDER BY created_at ASC`
	if groupID == nil {
		rows, err = d.Query(fmt.Sprintf(q, ""))
	} else {
		rows, err = d.Query(fmt.Sprintf(q, "WHERE group_id = ?"), *groupID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFeeds(rows)
}

// UpdateFeedMeta updates feed title and site URL after a successful fetch.
func (d *DB) UpdateFeedMeta(id int64, title, siteURL string) error {
	_, err := d.Exec(
		`UPDATE feeds SET title = ?, site_url = ?, last_fetched_at = ? WHERE id = ?`,
		title, siteURL, time.Now(), id,
	)
	return err
}

func (d *DB) countFeedsInGroup(groupID int64) (int, error) {
	var n int
	err := d.QueryRow(`SELECT COUNT(*) FROM feeds WHERE group_id = ?`, groupID).Scan(&n)
	return n, err
}

func scanFeeds(rows *sql.Rows) ([]Feed, error) {
	var feeds []Feed
	for rows.Next() {
		var f Feed
		var lastFetched sql.NullTime
		if err := rows.Scan(
			&f.ID, &f.GroupID, &f.URL, &f.Title, &f.SiteURL,
			&lastFetched, &f.FetchIntervalSeconds, &f.CreatedAt,
		); err != nil {
			return nil, err
		}
		if lastFetched.Valid {
			f.LastFetchedAt = &lastFetched.Time
		}
		feeds = append(feeds, f)
	}
	return feeds, rows.Err()
}
