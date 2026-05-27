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
  metadata_json TEXT,
  simhash TEXT
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

CREATE TABLE IF NOT EXISTS item_terms (
  item_id TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  value TEXT NOT NULL,
  weight REAL NOT NULL,
  PRIMARY KEY (item_id, kind, value)
);

CREATE VIRTUAL TABLE IF NOT EXISTS item_search USING fts5(
  item_id UNINDEXED,
  content,
  tokenize = 'unicode61'
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

CREATE TABLE IF NOT EXISTS item_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  item_id TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  event_type TEXT NOT NULL,
  source TEXT NOT NULL DEFAULT '',
  view TEXT NOT NULL DEFAULT '',
  occurred_at TEXT NOT NULL
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

CREATE TABLE IF NOT EXISTS recommendation_scores (
  item_id TEXT PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
  score REAL NOT NULL,
  interest_score REAL NOT NULL,
  hot_score REAL NOT NULL,
  reason_json TEXT,
  computed_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_items_last_seen ON items(last_seen_at DESC);
CREATE INDEX IF NOT EXISTS idx_items_published ON items(published_at DESC);
CREATE INDEX IF NOT EXISTS idx_items_canonical_url ON items(canonical_url);
CREATE INDEX IF NOT EXISTS idx_items_repo ON items(repo);
CREATE INDEX IF NOT EXISTS idx_items_arxiv ON items(arxiv_id);
CREATE INDEX IF NOT EXISTS idx_item_sources_item ON item_sources(item_id);
CREATE INDEX IF NOT EXISTS idx_item_sources_source_rank ON item_sources(source, source_view, source_rank);
CREATE INDEX IF NOT EXISTS idx_item_terms_lookup ON item_terms(kind, value);
CREATE INDEX IF NOT EXISTS idx_item_state_saved ON item_state(saved);
CREATE INDEX IF NOT EXISTS idx_item_state_read ON item_state(read);
CREATE INDEX IF NOT EXISTS idx_item_state_hidden ON item_state(hidden);
CREATE INDEX IF NOT EXISTS idx_item_events_item ON item_events(item_id);
CREATE INDEX IF NOT EXISTS idx_item_events_recent ON item_events(occurred_at DESC, event_type);
CREATE INDEX IF NOT EXISTS idx_fetch_history_started ON fetch_history(started_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_personalization_rules_enabled ON personalization_rules(enabled, effect, target);
CREATE INDEX IF NOT EXISTS idx_dedupe_candidates_score ON dedupe_candidates(score DESC, updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_recommendation_scores_score ON recommendation_scores(score DESC, computed_at DESC);

INSERT OR IGNORE INTO sources (id, name, status) VALUES
  ('hackernews', 'Hacker News', 'UNKNOWN'),
  ('github', 'GitHub Trending', 'UNKNOWN'),
  ('ailabs', 'AI Labs', 'UNKNOWN'),
  ('huggingface', 'Hugging Face Papers', 'UNKNOWN'),
  ('lobsters', 'Lobsters', 'UNKNOWN'),
  ('producthunt', 'Product Hunt', 'UNKNOWN');
