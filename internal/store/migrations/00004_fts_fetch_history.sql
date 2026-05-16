-- +goose Up
CREATE VIRTUAL TABLE IF NOT EXISTS item_search USING fts5(
  item_id UNINDEXED,
  content,
  tokenize = 'unicode61'
);

INSERT INTO item_search (item_id, content)
SELECT i.id,
       trim(
         COALESCE(i.title, '') || ' ' ||
         COALESCE(i.subtitle, '') || ' ' ||
         COALESCE(i.summary, '') || ' ' ||
         COALESCE(i.author, '') || ' ' ||
         COALESCE(i.organization, '') || ' ' ||
         COALESCE(i.language, '') || ' ' ||
         COALESCE(i.repo, '') || ' ' ||
         COALESCE(i.arxiv_id, '') || ' ' ||
         COALESCE(i.metadata_json, '') || ' ' ||
         COALESCE(tags.tags, '')
       )
FROM items i
LEFT JOIN (
  SELECT item_id, group_concat(tag, ' ') AS tags
  FROM item_tags
  GROUP BY item_id
) tags ON tags.item_id = i.id
WHERE NOT EXISTS (
  SELECT 1 FROM item_search s WHERE s.item_id = i.id
);

CREATE TABLE IF NOT EXISTS fetch_history (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source TEXT NOT NULL,
  source_view TEXT NOT NULL,
  status TEXT NOT NULL,
  started_at TEXT NOT NULL,
  finished_at TEXT NOT NULL,
  duration_ms INTEGER NOT NULL DEFAULT 0,
  item_count INTEGER NOT NULL DEFAULT 0,
  stale INTEGER NOT NULL DEFAULT 0,
  stale_reason TEXT,
  error TEXT
);

CREATE INDEX IF NOT EXISTS idx_fetch_history_started ON fetch_history(started_at DESC, id DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_fetch_history_started;
DROP TABLE IF EXISTS fetch_history;
DROP TABLE IF EXISTS item_search;
