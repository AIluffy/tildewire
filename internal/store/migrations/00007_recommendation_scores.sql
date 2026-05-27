-- +goose Up
CREATE TABLE IF NOT EXISTS recommendation_scores (
  item_id TEXT PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
  score REAL NOT NULL,
  interest_score REAL NOT NULL,
  hot_score REAL NOT NULL,
  computed_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_recommendation_scores_score ON recommendation_scores(score DESC, computed_at DESC);

-- +goose Down
DROP INDEX IF EXISTS idx_recommendation_scores_score;
DROP TABLE IF EXISTS recommendation_scores;
