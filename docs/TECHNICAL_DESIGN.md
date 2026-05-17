# tildewire Technical Design

> Version: v0.1 draft
> Date: 2026-05-10
> Stack: Go, Bubble Tea, Bubbles, Lip Gloss, Glamour, Huh, SQLite

## 1. Technical Goals

tildewire is a local-first terminal TUI. The technical design should optimize for fast startup, resilient source fetching, deterministic local state, and simple cross-platform distribution.

Primary technical goals:

- Single Go binary.
- TUI-only MVP.
- Cached startup before network refresh.
- Source adapters isolated from UI and storage concerns.
- SQLite persistence for cache and user state.
- Per-source rate limiting and stale cache fallback.
- Testable parsers and normalizers with fixtures.

## 2. Technology Choices

| Area | Choice | Reason |
|---|---|---|
| Language | Go | Single binary, concurrency, mature terminal tooling |
| TUI runtime | Bubble Tea | Elm-style terminal state machine |
| TUI components | Bubbles | List, viewport, text input, help, spinner |
| Styling | Lip Gloss | Terminal layout and theme system |
| Markdown | Glamour | Detail rendering for summaries and README-like content |
| Forms | Huh | First-run and settings forms |
| Storage | SQLite | Local cache and durable item state |
| SQLite driver | `modernc.org/sqlite` | CGo-free release builds |
| SQL access | `sqlc` | Typed SQL without hiding queries |
| Migrations | Embedded SQLite SQL | Small runtime migrator over versioned SQL files |
| HTTP retry | `go-retryablehttp` | Bounded retries and backoff |
| Rate limit | `golang.org/x/time/rate` | Per-source token buckets |
| HTML parsing | `goquery` | GitHub Trending parser |

## 3. Startup Template Decision

Use `charmbracelet/bubbletea-app-template` as the project starting point.

Rationale:

- It is aligned with a full-screen Bubble Tea app.
- It already includes Bubble Tea, Bubbles, and Lip Gloss.
- It includes GitHub Actions, GoReleaser, and golangci-lint config.
- It avoids a Cobra-first command tree before the product needs one.

MVP command shape:

```bash
tildewire
tildewire --config <path>
tildewire --debug
tildewire --version
tildewire --help
```

`tildewire` remains the canonical product and binary name.

Do not add `fetch`, `list`, `export`, `cache`, or `config` subcommands in v0.1. Keep those actions inside the TUI.

## 4. Architecture

```text
┌────────────────────────────────────────────────────────┐
│ TUI Layer                                               │
│ RootModel, views, keymap, layout, components            │
└───────────────────────────┬────────────────────────────┘
                            │ Msg / Cmd
┌───────────────────────────▼────────────────────────────┐
│ Application Layer                                       │
│ FeedService, RefreshService, SearchService, StateService│
└───────────────────────────┬────────────────────────────┘
                            │
┌───────────────────────────▼────────────────────────────┐
│ Domain Layer                                            │
│ FeedItem, ItemSource, Metrics, FetchScope, SourceID     │
└───────────────┬─────────────────────────────┬──────────┘
                │                             │
┌───────────────▼──────────────┐ ┌────────────▼──────────┐
│ Source Layer                  │ │ Store Layer            │
│ HN, GitHub, HF adapters       │ │ SQLite, sqlc, migrator  │
└───────────────┬──────────────┘ └────────────┬──────────┘
                │                             │
┌───────────────▼─────────────────────────────▼──────────┐
│ HTTP Layer                                               │
│ retry, cache, conditional requests, rate limits, auth    │
└──────────────────────────────────────────────────────────┘
```

Rules:

- Bubble Tea `Update()` must not do blocking I/O.
- TUI commands call application services.
- Source adapters fetch and normalize only.
- Sorting and dedupe are centralized.
- Store code owns SQLite transactions and migrations.

## 5. Data Flow

Startup:

```text
1. Parse minimal flags.
2. Load config.
3. Open SQLite database.
4. Run migrations.
5. Load cached normalized items.
6. Render TUI immediately.
7. Start background refresh for enabled sources.
8. Upsert fetched items and source metadata.
9. Re-score visible feed.
10. Send FeedUpdatedMsg to TUI.
```

Refresh:

```text
RefreshRequestedMsg
  -> tea.Cmd
  -> AppService.Refresh(ctx, scopes)
  -> SourceAdapter.Fetch
  -> SourceAdapter.Normalize
  -> Dedupe strong keys
  -> Store transaction
  -> FeedUpdatedMsg or SourceErrorMsg
```

## 6. Suggested Directory Structure

```text
tildewire/
  main.go
  go.mod
  go.sum
  scripts/
    install-local.sh
  docs/
    PRD.md
    TECHNICAL_DESIGN.md
    ROADMAP.md
  internal/
    boot/
      boot.go
      flags.go
    launcher/
      run.go
    app/
      service.go
      refresh.go
      feed.go
      search.go
      state.go
      errors.go
    config/
      config.go
      paths.go
      tokens.go
      defaults.go
    domain/
      item.go
      metrics.go
      scope.go
      source.go
      state.go
    tui/
      model.go
      messages.go
      keymap.go
      layout.go
      theme.go
      commands.go
      views/
      components/
    sources/
      adapter.go
      hackernews.go
      github_trending.go
      huggingface_papers.go
      fixtures/
    httpx/
      client.go
      cache.go
      rate_limit.go
      request_key.go
      retry.go
    normalize/
      url.go
      repo.go
      arxiv.go
      title.go
    score/
      hot.go
      native.go
    dedupe/
      canonical.go
      merge.go
    store/
      db.go
      migrations.go
      queries.sql
      schema.sql
      generated/
  migrations/
    00001_init.sql
  sqlc.yaml
  testdata/
    fixtures/
    golden/
```

## 7. Domain Model

### 7.1 FeedItem

```go
type FeedItem struct {
    ID           string
    CanonicalKey string

    Title        string
    Subtitle     string
    Summary      string
    URL          string
    CanonicalURL string
    CommentsURL  string

    ItemType     string // repo, story, paper
    Author       string
    Organization string
    Language     string
    Tags         []string

    PublishedAt *time.Time
    FirstSeenAt time.Time
    LastSeenAt  time.Time

    Metrics  Metrics
    Refs     Refs
    Metadata json.RawMessage
}
```

### 7.2 ItemSource

```go
type ItemSource struct {
    ItemID      string
    Source      SourceID
    SourceView  string
    SourceIDRaw string
    SourceRank  int
    SourceURL   string
    Metrics     Metrics
    Raw         json.RawMessage
    SeenAt      time.Time
}
```

### 7.3 Metrics

```go
type Metrics struct {
    Score       *float64
    Upvotes     *int64
    Comments    *int64
    Stars       *int64
    StarsToday  *int64
    Forks       *int64
    GitHubStars *int64
}
```

### 7.4 Refs

```go
type Refs struct {
    Repo    string
    ArxivID string
    PaperID string
    HNID    string
}
```

## 8. Source Adapter Interface

```go
type SourceID string

type SourceAdapter interface {
    Source() SourceID
    DefaultScopes() []FetchScope
    Fetch(ctx context.Context, scope FetchScope, client *httpx.Client) (*FetchResult, error)
    Normalize(ctx context.Context, scope FetchScope, raw *FetchResult) ([]FeedItem, error)
    CachePolicy(scope FetchScope) CachePolicy
}
```

```go
type FetchScope struct {
    Source   SourceID
    View     string
    Period   string
    Language string
    Topic    string
    Limit    int
    Params   map[string]string
}
```

## 9. MVP Source Designs

### 9.1 Hacker News

Endpoints:

```text
https://hacker-news.firebaseio.com/v0/topstories.json
https://hacker-news.firebaseio.com/v0/beststories.json
https://hacker-news.firebaseio.com/v0/newstories.json
https://hacker-news.firebaseio.com/v0/showstories.json
https://hacker-news.firebaseio.com/v0/item/{id}.json
```

Implementation:

- Fetch story id list.
- Limit default to 50.
- Fetch item details with concurrency 3-5.
- Preserve id list order as `source_rank`.
- Fetch comments only on detail view in later versions.

TTL:

- Story list: 5 minutes.
- Active story item: 10 minutes.
- Old story item: 24 hours.

### 9.2 GitHub Trending

Endpoints:

```text
https://github.com/trending
https://github.com/trending/{language}?since=daily
https://github.com/trending/{language}?since=weekly
```

Implementation:

- Parse HTML with `goquery`.
- Extract owner, repo, description, language, stars, forks, stars today, rank.
- Use GitHub REST API only for detail enrichment after MVP.

TTL:

- Daily: 30 minutes.
- Weekly: 2 hours.

Risk:

- HTML may change. Save raw HTTP cache and cover parser with fixtures.

### 9.3 Hugging Face Papers

Endpoint:

```text
https://huggingface.co/api/daily_papers
```

Implementation:

- Defensive JSON parsing.
- Treat fields as optional.
- Preserve raw JSON in `ItemSource`.
- Extract paper id, title, summary, authors, upvotes, comments, repo references where available.

TTL:

- Daily papers: 1 hour.
- Historical paper details: 7 days.

## 10. Cache and Persistence

Use stale-while-revalidate:

```text
1. Load normalized items from SQLite.
2. Render immediately.
3. Refresh in background.
4. Upsert new source data.
5. Recompute visible feed.
6. Keep old cache on failure.
```

Two cache layers:

- Raw HTTP cache: request, headers, body, status, ETag, Last-Modified.
- Normalized item cache: items, item_sources, tags, user state.

Startup SQLite PRAGMAs:

```sql
PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;
PRAGMA busy_timeout=5000;
PRAGMA synchronous=NORMAL;
```

## 11. SQLite Schema

Initial schema:

```sql
CREATE TABLE sources (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  enabled INTEGER NOT NULL DEFAULT 1,
  last_fetch_at TEXT,
  last_success_at TEXT,
  last_error TEXT,
  status TEXT NOT NULL DEFAULT 'unknown'
);

CREATE TABLE http_cache (
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

CREATE TABLE items (
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
  metadata_json TEXT
);

CREATE TABLE item_sources (
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

CREATE TABLE item_tags (
  item_id TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
  tag TEXT NOT NULL,
  PRIMARY KEY (item_id, tag)
);

CREATE TABLE item_state (
  item_id TEXT PRIMARY KEY REFERENCES items(id) ON DELETE CASCADE,
  read INTEGER NOT NULL DEFAULT 0,
  saved INTEGER NOT NULL DEFAULT 0,
  hidden INTEGER NOT NULL DEFAULT 0,
  read_at TEXT,
  saved_at TEXT,
  hidden_at TEXT,
  note TEXT
);

CREATE TABLE rate_limit_state (
  source TEXT NOT NULL,
  bucket TEXT NOT NULL,
  limit_value INTEGER,
  remaining INTEGER,
  reset_at TEXT,
  cooldown_until TEXT,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (source, bucket)
);
```

Recommended indexes:

```sql
CREATE INDEX idx_items_last_seen ON items(last_seen_at DESC);
CREATE INDEX idx_items_published ON items(published_at DESC);
CREATE INDEX idx_items_canonical_url ON items(canonical_url);
CREATE INDEX idx_items_repo ON items(repo);
CREATE INDEX idx_items_arxiv ON items(arxiv_id);
CREATE INDEX idx_item_sources_item ON item_sources(item_id);
CREATE INDEX idx_item_sources_source_rank ON item_sources(source, source_view, source_rank);
CREATE INDEX idx_item_state_saved ON item_state(saved);
CREATE INDEX idx_item_state_read ON item_state(read);
CREATE INDEX idx_item_state_hidden ON item_state(hidden);
```

## 12. Dedupe Strategy

v0.1 uses strong-key dedupe only:

Priority:

1. `repo:{owner}/{repo}`
2. `arxiv:{id}`
3. `paper:{id}`
4. `url:{canonical_url}`
5. `{source}:{source_id}`

URL canonicalization:

- Lowercase scheme and host.
- Remove default ports.
- Remove fragments.
- Normalize duplicate slashes.
- Remove tracking parameters such as `utm_*`, `ref`, `source`, `fbclid`, `gclid`.
- Sort query parameters.

v0.3 adds SimHash-assisted candidate detection on top of strong-key dedupe. Similar items are surfaced as non-destructive suggestions; ignoring a candidate records a local decision and does not merge or delete source context.

## 13. Sorting Strategy

Single-source views:

```sql
ORDER BY item_sources.source_rank ASC
```

All Hot v0.1:

```text
hot_score =
  0.55 * source_rank_score
+ 0.25 * recency_score
+ 0.15 * source_metric_score
+ 0.05 * cross_source_bonus
- read_penalty
```

All view scoring stays explainable and now includes v0.3 personalization adjustments from explicit boost/mute/hide rules plus saved/hidden preference signals. Single-source views continue to preserve source-native rank.

## 14. Rate Limits and Failure Handling

Default local limiters:

| Source | Limit |
|---|---|
| GitHub Trending HTML | 1 request / 30 seconds, burst 1 |
| HN API | 3 requests / second, burst 10 |
| HF Papers API | 1 request / 10 seconds, burst 2 |

Source states:

```text
OK
STALE
REFRESHING
RATE_LIMITED
AUTH_REQUIRED
PARSER_BROKEN
NETWORK_ERROR
DISABLED
```

Failure behavior:

| Failure | Behavior |
|---|---|
| 200 | Cache raw response, normalize, upsert |
| 304 | Extend cache TTL |
| 401 | Mark auth required |
| 403/429 | Persist cooldown, use stale cache |
| 5xx | Retry, then stale cache |
| Timeout | Retry, then stale cache |
| Parser error | Mark parser broken, keep raw response |

## 15. TUI Design

Root model responsibilities:

- Current page.
- Current filter.
- Current sort.
- Window size.
- Feed view model.
- Detail view model.
- Status bar.
- Keymap.

View modules:

- Feed view.
- Detail view.
- Filter modal.
- Help view.
- Status bar.
- Command palette in v0.2.

Rules:

- Render only visible rows.
- Cache expensive row rendering.
- Render Markdown only in detail view.
- Write logs to file, not stdout.

## 16. Config and Paths

Default paths:

```text
macOS/Linux:
  ~/.config/tildewire/config.toml
  ~/.local/share/tildewire/tildewire.db
  ~/.cache/tildewire/
  ~/.local/state/tildewire/debug.log

Windows:
  %APPDATA%\tildewire\config.toml
  %LOCALAPPDATA%\tildewire\tildewire.db
```

Config precedence:

```text
CLI flag > environment variable > config.toml > default
```

Token policy:

- v0.1 should not require tokens.
- Read optional tokens from `config.toml`, with environment variables taking precedence.
- Do not log tokens.
- Redact Authorization headers.
- Do not store tokens in SQLite.

## 17. Testing Strategy

Unit tests:

- URL canonicalization.
- Repo and arXiv extraction.
- Strong-key dedupe.
- Hot scoring.
- Rate-limit header parsing.
- Cache TTL.

Adapter fixture tests:

- HN story list and item JSON.
- GitHub Trending HTML.
- HF daily papers JSON.

Store tests:

- Migration from empty database.
- Upsert item.
- Upsert item source.
- Item state persistence.
- Source-native ordering.

TUI tests:

- Model update tests.
- Key handling tests.
- Layout does not panic across small and wide terminal sizes.
- Golden snapshots for important views.

Integration tests:

- Mock HTTP refresh pipeline.
- 429 cooldown.
- Stale cache fallback.
- Parser failure isolation.

## 18. Build and Release

Build:

```bash
go build -o tildewire .
```

Installers and package-manager formulas should install the canonical `tildewire` binary.

Release targets:

- macOS amd64/arm64.
- Linux amd64/arm64.
- Windows amd64/arm64.

Release distribution:

- GoReleaser publishes `tar.gz` archives for macOS and Linux, `zip` archives for Windows, and a `checksums.txt` file.
- GoReleaser also publishes Linux `.deb`, `.rpm`, and `.apk` packages attached to each GitHub Release.
- `scripts/install.sh` is the public macOS/Linux installer. It downloads a release archive, verifies the checksum, and installs `tildewire`.
- `scripts/install-local.sh` remains the local source-checkout installer for development.

Use tag pushes shaped like `v0.1.0` to trigger the release workflow. Package-manager repositories such as Homebrew taps, apt/yum repositories, Scoop, Winget, or Snap should be added only after their backing repository or registry credentials exist.

## 19. Main Technical Risks

| Risk | Mitigation |
|---|---|
| GitHub Trending HTML changes | Raw cache, fixture tests, parser status |
| TUI sluggishness | Visible-row rendering and cached row strings |
| SQLite locks | WAL, busy timeout, transactions |
| Source rate limits | Token buckets, TTL, cooldown persistence |
| Over-complex MVP | Ship three sources first |
| Product Hunt compliance | Keep out of required MVP |
