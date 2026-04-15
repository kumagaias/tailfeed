ALTER TABLE articles ADD COLUMN is_stocked INTEGER NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_articles_stocked ON articles(is_stocked) WHERE is_stocked = 1;
