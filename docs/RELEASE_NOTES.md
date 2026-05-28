# tildewire Release Notes

## v0.4.1

v0.4.1 is a patch release for feed ordering correctness and internal state-boundary hardening after v0.4.0.

### Fixed

- AI Labs source feeds now order items by published date from newest to oldest before source limits are applied.
- The AI Labs aggregate source view now preserves newest-first ordering across lab providers.
- App, TUI, HTTP cooldown, source navigation, and Markdown image-preview state boundaries were tightened with regression coverage.

## v0.4.0

v0.4.0 adds a local Recommend workflow on top of the existing multi-source feed. The release focuses on reducing repeat noise by learning from explicit local interactions while keeping source-specific views and privacy boundaries intact.

### Added

- Recommend tab with a top-10 locally ranked feed drawn from the latest cached source windows.
- Local interaction signals from saved, hidden, read, detail, open, source-open, URL-copy, and Markdown-copy actions.
- Time-decayed recommendation profile terms across keywords, tags, languages, repos, authors, organizations, and source context.
- Recommendation diagnostics panel for explaining profile signals, selected-item scoring, and exclusion reasons.
- Balanced recommendation selection so strong signals from one source do not crowd out every other eligible source.
- Runtime source/token settings hardening so source visibility, Product Hunt credentials, and HTTP cache TTL changes apply without rebuilding the service.

### Notes

- Recommendation data stays local in SQLite; no remote account or sync service is introduced.
- Recommend excludes durable hidden items and hide-rule matches even when Show hidden items is enabled.
- No new CLI subcommands were added; recommendation and diagnostics workflows stay inside the TUI.

## v0.3.0

v0.3.0 completes the personalization and observability slice for repeat users who want less noise, clearer source health, and first-class AI lab news in the daily radar.

### Added

- SQLite FTS5-backed search for title, summary, metadata, tags, repository refs, author, organization, and source-specific identifiers.
- Source health and recent fetch history in the `!` panel, including status, timing, stale reasons, item counts, and recent errors.
- Personalization rules for boost, mute, and hide effects across keyword, language, tag, domain, source, repo, and author targets.
- A TUI rule form for adding and editing personalization rules, plus palette shortcuts for quick rules from the current item or search.
- Saved/hidden preference signals that adjust All-view ranking while preserving source-native ranking in single-source views.
- SimHash-assisted dedupe candidates with debug details and non-destructive ignore decisions.
- AI Labs source aggregating OpenAI News, Anthropic News, Google DeepMind News, and Meta AI Blog.
- Source Health retry via `r` after refresh failures, with forced visible-scope refresh.

### Notes

- Dedupe candidates are suggestions only; tildewire does not silently merge or delete source context.
- AI Labs defaults to the aggregate source feed; press `v` to narrow it to a single lab scope.
- No new CLI subcommands were added; personalization and dedupe workflows stay inside the TUI.

## v0.2 draft

v0.2 expands the TUI-only daily radar with a new source, richer detail views, saved-item export, command palette access, and a more capable filter modal.

### Added

- Lobsters source with `hottest` and `newest` views.
- Lazy detail enrichment for Hacker News comments, Lobsters comments, GitHub README previews, and Hugging Face paper metadata.
- Saved-item export from the command palette in Markdown, JSON, and CSV.
- Command palette with export, refresh, item actions, source switching, filter, settings, and source health commands.
- Filter modal source and scope controls, independent saved/unread toggles, dynamic language/tag facets from the current feed, and include-hidden filtering.
- Scrollable detail view content with comment metadata, comment links, item tags, and detail provider labels.
- Optional Product Hunt source with token-gated refresh, visible `AUTH_REQUIRED` status, and Product Hunt post URL attribution.

### Notes

- Product Hunt stays visible in the TUI when no token is configured, but refresh is skipped until `PRODUCT_HUNT_TOKEN` is set.
- VHS demo recording is outside the v0.2 completion scope.
- No new CLI subcommands were added; export and source/detail workflows stay inside the TUI.
