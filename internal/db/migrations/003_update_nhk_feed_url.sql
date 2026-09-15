-- If both URLs are registered, merge the legacy feed into the replacement
-- before removing it. The articles table cascades on feed deletion, so
-- deleting the legacy feed first would erase its saved history.
UPDATE feeds
SET group_id = COALESCE(
    group_id,
    (SELECT legacy.group_id FROM feeds AS legacy
     WHERE legacy.url = 'https://www3.nhk.or.jp/rss/news/cat06.xml')
)
WHERE url = 'https://news.web.nhk/n-data/conf/na/rss/cat0.xml'
  AND EXISTS (
    SELECT 1 FROM feeds
    WHERE url = 'https://www3.nhk.or.jp/rss/news/cat06.xml'
  );

DELETE FROM articles
WHERE id IN (
    SELECT legacy.id
    FROM articles AS legacy
    WHERE legacy.feed_id = (
        SELECT id FROM feeds
        WHERE url = 'https://www3.nhk.or.jp/rss/news/cat06.xml'
    )
      AND EXISTS (
        SELECT 1 FROM articles AS current
        WHERE current.feed_id = (
            SELECT id FROM feeds
            WHERE url = 'https://news.web.nhk/n-data/conf/na/rss/cat0.xml'
        )
          AND current.guid = legacy.guid
      )
);

UPDATE articles
SET feed_id = (
    SELECT id FROM feeds
    WHERE url = 'https://news.web.nhk/n-data/conf/na/rss/cat0.xml'
)
WHERE feed_id = (
    SELECT id FROM feeds
    WHERE url = 'https://www3.nhk.or.jp/rss/news/cat06.xml'
)
  AND EXISTS (
    SELECT 1 FROM feeds
    WHERE url = 'https://news.web.nhk/n-data/conf/na/rss/cat0.xml'
  );

DELETE FROM feeds
WHERE url = 'https://www3.nhk.or.jp/rss/news/cat06.xml'
  AND EXISTS (
    SELECT 1 FROM feeds
    WHERE url = 'https://news.web.nhk/n-data/conf/na/rss/cat0.xml'
  );

UPDATE feeds
SET url = 'https://news.web.nhk/n-data/conf/na/rss/cat0.xml',
    title = NULL,
    site_url = NULL,
    last_fetched_at = NULL
WHERE url = 'https://www3.nhk.or.jp/rss/news/cat06.xml';
