# tildewire Release Notes

## v0.3 draft

v0.3 completes the personalization and observability slice for repeat users who want less noise and clearer source health.

### Added

- Optional `tw` short entrypoint for daily terminal use, sharing the same TUI, flags, config, cache, data, and state paths as `tildewire`; local installation skips `tw` when the name is already taken.
- SQLite FTS5-backed search for title, summary, metadata, tags, repository refs, author, organization, and source-specific identifiers.
- Source health and recent fetch history in the `!` panel, including status, timing, stale reasons, item counts, and recent errors.
- Personalization rules for boost, mute, and hide effects across keyword, language, tag, domain, source, repo, and author targets.
- A TUI rule form for adding and editing personalization rules, plus palette shortcuts for quick rules from the current item or search.
- Saved/hidden preference signals that adjust All-view ranking while preserving source-native ranking in single-source views.
- SimHash-assisted dedupe candidates with debug details and non-destructive ignore decisions.

### Notes

- Dedupe candidates are suggestions only; tildewire does not silently merge or delete source context.
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
