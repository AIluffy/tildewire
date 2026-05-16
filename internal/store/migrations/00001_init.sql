-- +goose Up
-- +goose NO TRANSACTION
PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;
PRAGMA busy_timeout=5000;
PRAGMA synchronous=NORMAL;

CREATE TABLE IF NOT EXISTS sources (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  last_fetch_at TEXT,
  last_success_at TEXT,
  last_error TEXT,
  status TEXT NOT NULL DEFAULT 'UNKNOWN'
);

CREATE TABLE IF NOT EXISTS http_cache (
  request_key TEXT PRIMARY KEY,
  source TEXT NOT NULL,
  method TEXT NOT NULL,
  url TEXT NOT NULL,
  status_code INTEGER,
  headers_json TEXT,
  body BLOB,
  fetched_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  etag TEXT,
  last_modified TEXT
);

CREATE TABLE IF NOT EXISTS items (
  id TEXT PRIMARY KEY,
  canonical_key TEXT NOT NULL UNIQUE,
  title TEXT NOT NULL,
  subtitle TEXT,
  summary TEXT,
  url TEXT,
  canonical_url TEXT,
  comments_url TEXT,
  item_type TEXT NOT NULL,
  author TEXT,
  organization TEXT,
  language TEXT,
  published_at TEXT,
  first_seen_at TEXT NOT NULL,
  last_seen_at TEXT NOT NULL,
  repo TEXT,
  arxiv_id TEXT,
  metrics_json TEXT,
  refs_json TEXT,
  metadata_json TEXT
);

CREATE TABLE IF NOT EXISTS item_sources (
  item_id TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  source TEXT NOT NULL,
  source_view TEXT NOT NULL,
  source_id TEXT NOT NULL,
  source_rank INTEGER NOT NULL,
  source_url TEXT,
  metrics_json TEXT,
  raw_json TEXT,
  seen_at TEXT NOT NULL,
  PRIMARY KEY (source, source_view, source_id)
);

CREATE TABLE IF NOT EXISTS item_tags (
  item_id TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  tag TEXT NOT NULL,
  PRIMARY KEY (item_id, tag)
);

CREATE TABLE IF NOT EXISTS item_state (
  item_id TEXT PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
  read INTEGER NOT NULL DEFAULT 0,
  saved INTEGER NOT NULL DEFAULT 0,
  hidden INTEGER NOT NULL DEFAULT 0,
  read_at TEXT,
  saved_at TEXT,
  hidden_at TEXT,
  note TEXT
);

CREATE TABLE IF NOT EXISTS rate_limit_state (
  source TEXT NOT NULL,
  bucket TEXT NOT NULL,
  limit_value INTEGER,
  remaining INTEGER,
  reset_at TEXT,
  cooldown_until TEXT,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (source, bucket)
);

CREATE INDEX IF NOT EXISTS idx_items_last_seen ON items(last_seen_at DESC);
CREATE INDEX IF NOT EXISTS idx_items_published ON items(published_at DESC);
CREATE INDEX IF NOT EXISTS idx_items_canonical_url ON items(canonical_url);
CREATE INDEX IF NOT EXISTS idx_items_repo ON items(repo);
CREATE INDEX IF NOT EXISTS idx_items_arxiv ON items(arxiv_id);
CREATE INDEX IF NOT EXISTS idx_item_sources_item ON item_sources(item_id);
CREATE INDEX IF NOT EXISTS idx_item_sources_source_rank ON item_sources(source, source_view, source_rank);
CREATE INDEX IF NOT EXISTS idx_item_state_saved ON item_state(saved);
CREATE INDEX IF NOT EXISTS idx_item_state_read ON item_state(read);
CREATE INDEX IF NOT EXISTS idx_item_state_hidden ON item_state(hidden);

INSERT OR IGNORE INTO sources (id, name, status) VALUES
  ('hackernews', 'Hacker News', 'UNKNOWN'),
  ('github', 'GitHub Trending', 'UNKNOWN'),
  ('huggingface', 'Hugging Face Papers', 'UNKNOWN'),
  ('lobsters', 'Lobsters', 'UNKNOWN'),
  ('producthunt', 'Product Hunt', 'UNKNOWN');

-- +goose Down
DROP TABLE IF EXISTS rate_limit_state;
DROP TABLE IF EXISTS item_state;
DROP TABLE IF EXISTS item_tags;
DROP TABLE IF EXISTS item_sources;
DROP TABLE IF EXISTS items;
DROP TABLE IF EXISTS http_cache;
DROP TABLE IF EXISTS sources;
