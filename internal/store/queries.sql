-- name: ListFeed :many
SELECT i.id, i.canonical_key, i.title, i.subtitle, i.summary, i.url, i.canonical_url, i.comments_url,
       i.item_type, i.author, i.organization, i.language, i.published_at, i.first_seen_at, i.last_seen_at,
       i.repo, i.arxiv_id, i.metrics_json, i.refs_json, i.metadata_json, COALESCE(i.simhash, '') AS simhash,
       COALESCE(st.read, 0) AS read, COALESCE(st.saved, 0) AS saved, COALESCE(st.hidden, 0) AS hidden,
       st.read_at, st.saved_at, st.hidden_at, COALESCE(st.note, '') AS note
FROM items i
LEFT JOIN item_state st ON st.item_id = i.id
WHERE (sqlc.arg(include_hidden) = 1 OR COALESCE(st.hidden, 0) = 0)
  AND (sqlc.arg(source) = '' OR EXISTS (
    SELECT 1 FROM item_sources src
    WHERE src.item_id = i.id
      AND src.source = sqlc.arg(source)
      AND (sqlc.arg(source_view) = '' OR lower(src.source_view) = sqlc.arg(source_view))
  ))
  AND (sqlc.arg(saved_only) = 0 OR COALESCE(st.saved, 0) = 1)
  AND (sqlc.arg(unread_only) = 0 OR COALESCE(st.read, 0) = 0)
  AND (sqlc.arg(language) = '' OR lower(COALESCE(i.language, '')) = sqlc.arg(language))
  AND (sqlc.arg(tag) = '' OR EXISTS (
    SELECT 1 FROM item_tags tag WHERE tag.item_id = i.id AND lower(tag.tag) = sqlc.arg(tag)
  ))
  AND (sqlc.arg(search) = '' OR EXISTS (
    SELECT 1 FROM item_search search
    WHERE search.item_id = i.id
      AND item_search MATCH sqlc.arg(search)
    LIMIT 1
  )
  )
ORDER BY i.last_seen_at DESC
LIMIT sqlc.arg(limit);

-- name: UpsertItem :exec
INSERT INTO items (id, canonical_key, title, subtitle, summary, url, canonical_url, comments_url, item_type, author, organization, language,
                   published_at, first_seen_at, last_seen_at, repo, arxiv_id, metrics_json, refs_json, metadata_json, simhash)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(canonical_key) DO UPDATE SET
  title = excluded.title,
  subtitle = excluded.subtitle,
  summary = excluded.summary,
  url = excluded.url,
  canonical_url = excluded.canonical_url,
  comments_url = excluded.comments_url,
  item_type = excluded.item_type,
  author = excluded.author,
  organization = excluded.organization,
  language = excluded.language,
  published_at = excluded.published_at,
  last_seen_at = excluded.last_seen_at,
  repo = excluded.repo,
  arxiv_id = excluded.arxiv_id,
  metrics_json = excluded.metrics_json,
  refs_json = excluded.refs_json,
  metadata_json = excluded.metadata_json,
  simhash = excluded.simhash;

-- name: EnsureItemState :exec
INSERT OR IGNORE INTO item_state (item_id) VALUES (?);

-- name: DeleteItemTags :exec
DELETE FROM item_tags WHERE item_id = ?;

-- name: InsertItemTag :exec
INSERT OR IGNORE INTO item_tags (item_id, tag) VALUES (?, ?);

-- name: DeleteItemSearch :exec
DELETE FROM item_search WHERE item_id = ?;

-- name: InsertItemSearch :exec
INSERT INTO item_search (item_id, content) VALUES (?, ?);

-- name: UpsertItemSource :exec
INSERT INTO item_sources (item_id, source, source_view, source_id, source_rank, source_url, metrics_json, raw_json, seen_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(source, source_view, source_id) DO UPDATE SET
  item_id = excluded.item_id,
  source_rank = excluded.source_rank,
  source_url = excluded.source_url,
  metrics_json = excluded.metrics_json,
  raw_json = excluded.raw_json,
  seen_at = excluded.seen_at;

-- name: ListItemSources :many
SELECT source, source_view, source_id, source_rank, COALESCE(source_url, '') AS source_url,
       COALESCE(metrics_json, '') AS metrics_json, COALESCE(raw_json, '') AS raw_json, seen_at
FROM item_sources
WHERE item_id = ?
ORDER BY source_rank ASC;

-- name: ListItemTags :many
SELECT tag FROM item_tags WHERE item_id = ? ORDER BY tag;

-- name: SetSavedState :exec
UPDATE item_state SET saved = ?, saved_at = ? WHERE item_id = ?;

-- name: SetReadState :exec
UPDATE item_state SET read = ?, read_at = ? WHERE item_id = ?;

-- name: SetHiddenState :exec
UPDATE item_state SET hidden = ?, hidden_at = ? WHERE item_id = ?;

-- name: ListSourceStatuses :many
SELECT id, name, status, last_fetch_at, last_success_at, COALESCE(last_error, '') AS last_error
FROM sources
ORDER BY CASE id WHEN 'github' THEN 1 WHEN 'huggingface' THEN 2 WHEN 'lobsters' THEN 3 WHEN 'producthunt' THEN 4 WHEN 'hackernews' THEN 5 ELSE 6 END;

-- name: UpsertSourceStatus :exec
INSERT INTO sources (id, name, last_fetch_at, last_success_at, last_error, status)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
  last_fetch_at = excluded.last_fetch_at,
  last_success_at = CASE WHEN excluded.last_success_at IS NULL THEN sources.last_success_at ELSE excluded.last_success_at END,
  last_error = excluded.last_error,
  status = excluded.status;

-- name: InsertFetchEvent :exec
INSERT INTO fetch_history (source, source_view, status, started_at, finished_at, duration_ms, item_count, stale, stale_reason, error)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: ListRecentFetchEvents :many
SELECT source, source_view, status, started_at, finished_at, duration_ms, item_count, stale,
       COALESCE(stale_reason, '') AS stale_reason, COALESCE(error, '') AS error
FROM fetch_history
ORDER BY started_at DESC, id DESC
LIMIT ?;

-- name: PruneFetchHistory :exec
DELETE FROM fetch_history
WHERE id NOT IN (
  SELECT id
  FROM fetch_history
  ORDER BY started_at DESC, id DESC
  LIMIT ?
);

-- name: CreatePersonalizationRule :one
INSERT INTO personalization_rules (effect, target, value, enabled, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
RETURNING id, effect, target, value, enabled, created_at, updated_at;

-- name: UpdatePersonalizationRule :one
UPDATE personalization_rules
SET effect = ?, target = ?, value = ?, enabled = ?, updated_at = ?
WHERE id = ?
RETURNING id, effect, target, value, enabled, created_at, updated_at;

-- name: ListPersonalizationRules :many
SELECT id, effect, target, value, enabled, created_at, updated_at
FROM personalization_rules
WHERE (sqlc.arg(include_disabled) = 1 OR enabled = 1)
ORDER BY enabled DESC, effect, target, value, id;

-- name: SetPersonalizationRuleEnabled :exec
UPDATE personalization_rules
SET enabled = ?, updated_at = ?
WHERE id = ?;

-- name: DeletePersonalizationRule :exec
DELETE FROM personalization_rules WHERE id = ?;

-- name: ListPreferenceSignals :many
SELECT state, target, value, total
FROM (
  SELECT 'saved' AS state, 'language' AS target, lower(i.language) AS value, COUNT(*) AS total
  FROM item_state st
  JOIN items i ON i.id = st.item_id
  WHERE st.saved = 1 AND COALESCE(i.language, '') <> ''
  GROUP BY lower(i.language)
  UNION ALL
  SELECT 'hidden' AS state, 'language' AS target, lower(i.language) AS value, COUNT(*) AS total
  FROM item_state st
  JOIN items i ON i.id = st.item_id
  WHERE st.hidden = 1 AND COALESCE(i.language, '') <> ''
  GROUP BY lower(i.language)
  UNION ALL
  SELECT 'saved' AS state, 'repo' AS target, lower(i.repo) AS value, COUNT(*) AS total
  FROM item_state st
  JOIN items i ON i.id = st.item_id
  WHERE st.saved = 1 AND COALESCE(i.repo, '') <> ''
  GROUP BY lower(i.repo)
  UNION ALL
  SELECT 'hidden' AS state, 'repo' AS target, lower(i.repo) AS value, COUNT(*) AS total
  FROM item_state st
  JOIN items i ON i.id = st.item_id
  WHERE st.hidden = 1 AND COALESCE(i.repo, '') <> ''
  GROUP BY lower(i.repo)
  UNION ALL
  SELECT 'saved' AS state, 'author' AS target, lower(COALESCE(NULLIF(i.author, ''), i.organization)) AS value, COUNT(*) AS total
  FROM item_state st
  JOIN items i ON i.id = st.item_id
  WHERE st.saved = 1 AND COALESCE(NULLIF(i.author, ''), i.organization, '') <> ''
  GROUP BY lower(COALESCE(NULLIF(i.author, ''), i.organization))
  UNION ALL
  SELECT 'hidden' AS state, 'author' AS target, lower(COALESCE(NULLIF(i.author, ''), i.organization)) AS value, COUNT(*) AS total
  FROM item_state st
  JOIN items i ON i.id = st.item_id
  WHERE st.hidden = 1 AND COALESCE(NULLIF(i.author, ''), i.organization, '') <> ''
  GROUP BY lower(COALESCE(NULLIF(i.author, ''), i.organization))
  UNION ALL
  SELECT 'saved' AS state, 'tag' AS target, lower(tag.tag) AS value, COUNT(*) AS total
  FROM item_state st
  JOIN item_tags tag ON tag.item_id = st.item_id
  WHERE st.saved = 1
  GROUP BY lower(tag.tag)
  UNION ALL
  SELECT 'hidden' AS state, 'tag' AS target, lower(tag.tag) AS value, COUNT(*) AS total
  FROM item_state st
  JOIN item_tags tag ON tag.item_id = st.item_id
  WHERE st.hidden = 1
  GROUP BY lower(tag.tag)
  UNION ALL
  SELECT 'saved' AS state, 'source' AS target, lower(src.source) AS value, COUNT(DISTINCT st.item_id) AS total
  FROM item_state st
  JOIN item_sources src ON src.item_id = st.item_id
  WHERE st.saved = 1
  GROUP BY lower(src.source)
  UNION ALL
  SELECT 'hidden' AS state, 'source' AS target, lower(src.source) AS value, COUNT(DISTINCT st.item_id) AS total
  FROM item_state st
  JOIN item_sources src ON src.item_id = st.item_id
  WHERE st.hidden = 1
  GROUP BY lower(src.source)
)
WHERE value <> '';

-- name: ListDedupeItems :many
SELECT id, canonical_key, title, COALESCE(subtitle, '') AS subtitle, COALESCE(summary, '') AS summary,
       COALESCE(simhash, '') AS simhash, last_seen_at
FROM items
WHERE COALESCE(simhash, '') <> ''
ORDER BY last_seen_at DESC
LIMIT ?;

-- name: UpsertDedupeCandidate :exec
INSERT INTO dedupe_candidates (key, item_id_a, item_id_b, score, distance, reason, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(key) DO UPDATE SET
  score = excluded.score,
  distance = excluded.distance,
  reason = excluded.reason,
  updated_at = excluded.updated_at;

-- name: ListDedupeCandidates :many
SELECT c.key, c.score, c.distance, c.reason, c.created_at, c.updated_at,
       a.id AS item_a_id, a.canonical_key AS item_a_canonical_key, a.title AS item_a_title,
       COALESCE(a.subtitle, '') AS item_a_subtitle, COALESCE(a.summary, '') AS item_a_summary,
       COALESCE(a.url, '') AS item_a_url, COALESCE(a.canonical_url, '') AS item_a_canonical_url,
       COALESCE(a.comments_url, '') AS item_a_comments_url, a.item_type AS item_a_item_type,
       COALESCE(a.author, '') AS item_a_author, COALESCE(a.organization, '') AS item_a_organization,
       COALESCE(a.language, '') AS item_a_language, a.first_seen_at AS item_a_first_seen_at,
       a.last_seen_at AS item_a_last_seen_at, COALESCE(a.repo, '') AS item_a_repo,
       COALESCE(a.arxiv_id, '') AS item_a_arxiv_id, COALESCE(a.metadata_json, '') AS item_a_metadata_json,
       COALESCE(a.simhash, '') AS item_a_simhash,
       COALESCE(sa.source, '') AS item_a_source, COALESCE(sa.source_view, '') AS item_a_source_view,
       COALESCE(sa.source_id, '') AS item_a_source_id, COALESCE(sa.source_rank, 0) AS item_a_source_rank,
       COALESCE(sa.source_url, '') AS item_a_source_url, COALESCE(sa.seen_at, '') AS item_a_source_seen_at,
       b.id AS item_b_id, b.canonical_key AS item_b_canonical_key, b.title AS item_b_title,
       COALESCE(b.subtitle, '') AS item_b_subtitle, COALESCE(b.summary, '') AS item_b_summary,
       COALESCE(b.url, '') AS item_b_url, COALESCE(b.canonical_url, '') AS item_b_canonical_url,
       COALESCE(b.comments_url, '') AS item_b_comments_url, b.item_type AS item_b_item_type,
       COALESCE(b.author, '') AS item_b_author, COALESCE(b.organization, '') AS item_b_organization,
       COALESCE(b.language, '') AS item_b_language, b.first_seen_at AS item_b_first_seen_at,
       b.last_seen_at AS item_b_last_seen_at, COALESCE(b.repo, '') AS item_b_repo,
       COALESCE(b.arxiv_id, '') AS item_b_arxiv_id, COALESCE(b.metadata_json, '') AS item_b_metadata_json,
       COALESCE(b.simhash, '') AS item_b_simhash,
       COALESCE(sb.source, '') AS item_b_source, COALESCE(sb.source_view, '') AS item_b_source_view,
       COALESCE(sb.source_id, '') AS item_b_source_id, COALESCE(sb.source_rank, 0) AS item_b_source_rank,
       COALESCE(sb.source_url, '') AS item_b_source_url, COALESCE(sb.seen_at, '') AS item_b_source_seen_at
FROM dedupe_candidates c
JOIN items a ON a.id = c.item_id_a
JOIN items b ON b.id = c.item_id_b
LEFT JOIN item_sources sa ON sa.rowid = (
  SELECT source.rowid
  FROM item_sources source
  WHERE source.item_id = a.id
  ORDER BY CASE WHEN source.source_rank > 0 THEN 0 ELSE 1 END, source.source_rank, source.source
  LIMIT 1
)
LEFT JOIN item_sources sb ON sb.rowid = (
  SELECT source.rowid
  FROM item_sources source
  WHERE source.item_id = b.id
  ORDER BY CASE WHEN source.source_rank > 0 THEN 0 ELSE 1 END, source.source_rank, source.source
  LIMIT 1
)
LEFT JOIN dedupe_decisions d ON d.candidate_key = c.key AND d.decision = 'ignore'
WHERE d.candidate_key IS NULL
ORDER BY c.score DESC, c.updated_at DESC
LIMIT ?;

-- name: IgnoreDedupeCandidate :exec
INSERT INTO dedupe_decisions (candidate_key, decision, decided_at)
VALUES (?, 'ignore', ?)
ON CONFLICT(candidate_key) DO UPDATE SET
  decision = 'ignore',
  decided_at = excluded.decided_at;

-- name: SourceCounts :many
SELECT source, total
FROM (
  SELECT 'all' AS source, COUNT(DISTINCT i.id) AS total
  FROM items i
  LEFT JOIN item_state st ON st.item_id = i.id
  WHERE COALESCE(st.hidden, 0) = 0
  UNION ALL
  SELECT src.source AS source, COUNT(DISTINCT src.item_id) AS total
  FROM item_sources src
  JOIN items i ON i.id = src.item_id
  LEFT JOIN item_state st ON st.item_id = src.item_id
  WHERE COALESCE(st.hidden, 0) = 0
  GROUP BY src.source
);

-- name: SourceViewCount :one
SELECT COUNT(DISTINCT src.item_id) AS total
FROM item_sources src
JOIN items i ON i.id = src.item_id
LEFT JOIN item_state st ON st.item_id = src.item_id
WHERE src.source = sqlc.arg(source)
  AND lower(src.source_view) = sqlc.arg(source_view)
  AND COALESCE(st.hidden, 0) = 0;

-- name: SourceViewCounts :many
SELECT src.source, lower(src.source_view) AS source_view, COUNT(DISTINCT src.item_id) AS total
FROM item_sources src
JOIN items i ON i.id = src.item_id
LEFT JOIN item_state st ON st.item_id = src.item_id
WHERE COALESCE(st.hidden, 0) = 0
GROUP BY src.source, lower(src.source_view);

-- name: UpsertHTTPCache :exec
INSERT INTO http_cache (request_key, source, method, url, status_code, headers_json, body, fetched_at, expires_at, etag, last_modified)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(request_key) DO UPDATE SET
  source = excluded.source,
  method = excluded.method,
  url = excluded.url,
  status_code = excluded.status_code,
  headers_json = excluded.headers_json,
  body = excluded.body,
  fetched_at = excluded.fetched_at,
  expires_at = excluded.expires_at,
  etag = excluded.etag,
  last_modified = excluded.last_modified;

-- name: GetHTTPCache :one
SELECT request_key, source, method, url, COALESCE(status_code, 0) AS status_code,
       COALESCE(headers_json, '') AS headers_json, body, fetched_at, expires_at,
       COALESCE(etag, '') AS etag, COALESCE(last_modified, '') AS last_modified
FROM http_cache
WHERE request_key = ?;

-- name: ClearHTTPCache :exec
DELETE FROM http_cache;

-- name: ClearRateLimitState :exec
DELETE FROM rate_limit_state;

-- name: ClearDedupeCandidates :exec
DELETE FROM dedupe_candidates;

-- name: ClearUnsavedItemSearch :exec
DELETE FROM item_search
WHERE item_id NOT IN (
  SELECT item_id FROM item_state WHERE saved = 1
);

-- name: ClearUnsavedItems :exec
DELETE FROM items
WHERE id NOT IN (
  SELECT item_id FROM item_state WHERE saved = 1
);

-- name: ResetSourceStatuses :exec
UPDATE sources
SET last_fetch_at = NULL,
    last_success_at = NULL,
    last_error = NULL,
    status = 'UNKNOWN';

-- name: GetRateLimitCooldown :one
SELECT COALESCE(cooldown_until, '') AS cooldown_until
FROM rate_limit_state
WHERE source = ? AND bucket = ?;

-- name: UpsertRateLimitCooldown :exec
INSERT INTO rate_limit_state (source, bucket, cooldown_until, updated_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(source, bucket) DO UPDATE SET
  cooldown_until = excluded.cooldown_until,
  updated_at = excluded.updated_at;
