-- +goose Up
ALTER TABLE items ADD COLUMN simhash TEXT;

CREATE TABLE IF NOT EXISTS personalization_rules (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  effect TEXT NOT NULL,
  target TEXT NOT NULL,
  value TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS dedupe_candidates (
  key TEXT PRIMARY KEY,
  item_id_a TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  item_id_b TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  score REAL NOT NULL,
  distance INTEGER NOT NULL,
  reason TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS dedupe_decisions (
  candidate_key TEXT PRIMARY KEY,
  decision TEXT NOT NULL,
  decided_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_personalization_rules_enabled ON personalization_rules(enabled, effect, target);
CREATE INDEX IF NOT EXISTS idx_dedupe_candidates_score ON dedupe_candidates(score DESC, updated_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_dedupe_candidates_score;
DROP INDEX IF EXISTS idx_personalization_rules_enabled;
DROP TABLE IF EXISTS dedupe_decisions;
DROP TABLE IF EXISTS dedupe_candidates;
DROP TABLE IF EXISTS personalization_rules;
