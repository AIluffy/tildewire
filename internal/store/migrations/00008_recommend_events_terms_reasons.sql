-- +goose Up
CREATE TABLE IF NOT EXISTS item_terms (
  item_id TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  value TEXT NOT NULL,
  weight REAL NOT NULL,
  PRIMARY KEY (item_id, kind, value)
);

CREATE TABLE IF NOT EXISTS item_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  item_id TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  event_type TEXT NOT NULL,
  source TEXT NOT NULL DEFAULT '',
  view TEXT NOT NULL DEFAULT '',
  occurred_at TEXT NOT NULL
);

DROP INDEX IF EXISTS idx_recommendation_scores_score;

CREATE TABLE IF NOT EXISTS recommendation_scores_next (
  item_id TEXT PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
  score REAL NOT NULL,
  interest_score REAL NOT NULL,
  hot_score REAL NOT NULL,
  reason_json TEXT,
  computed_at TEXT NOT NULL
);

INSERT OR REPLACE INTO recommendation_scores_next (item_id, score, interest_score, hot_score, reason_json, computed_at)
SELECT item_id, score, interest_score, hot_score, NULL, computed_at
FROM recommendation_scores;

DROP TABLE recommendation_scores;
ALTER TABLE recommendation_scores_next RENAME TO recommendation_scores;

CREATE INDEX IF NOT EXISTS idx_item_terms_lookup ON item_terms(kind, value);
CREATE INDEX IF NOT EXISTS idx_item_events_item ON item_events(item_id);
CREATE INDEX IF NOT EXISTS idx_item_events_recent ON item_events(occurred_at DESC, event_type);
CREATE INDEX IF NOT EXISTS idx_recommendation_scores_score ON recommendation_scores(score DESC, computed_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_item_events_recent;
DROP INDEX IF EXISTS idx_item_events_item;
DROP INDEX IF EXISTS idx_item_terms_lookup;
DROP INDEX IF EXISTS idx_recommendation_scores_score;
DROP TABLE IF EXISTS item_events;
DROP TABLE IF EXISTS item_terms;
