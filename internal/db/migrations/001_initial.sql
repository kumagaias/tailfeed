CREATE TABLE IF NOT EXISTS groups (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT    NOT NULL UNIQUE,
    position   INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS feeds (
    id                     INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id               INTEGER REFERENCES groups(id) ON DELETE SET NULL,
    url                    TEXT    NOT NULL UNIQUE,
    title                  TEXT,
    site_url               TEXT,
    last_fetched_at        DATETIME,
    fetch_interval_seconds INTEGER NOT NULL DEFAULT 900,
    created_at             DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS articles (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    feed_id      INTEGER NOT NULL REFERENCES feeds(id) ON DELETE CASCADE,
    guid         TEXT    NOT NULL,
    title        TEXT    NOT NULL,
    link         TEXT,
    summary      TEXT,
    published_at DATETIME,
    is_read      INTEGER NOT NULL DEFAULT 0,
    created_at   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(feed_id, guid)
);

CREATE INDEX IF NOT EXISTS idx_articles_feed_published ON articles(feed_id, published_at DESC);
CREATE INDEX IF NOT EXISTS idx_feeds_group_id ON feeds(group_id);
