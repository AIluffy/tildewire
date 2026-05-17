# tildewire Product Roadmap

> Version: v0.1 draft
> Date: 2026-05-10

## 1. Roadmap Strategy

tildewire should evolve from a focused daily terminal radar into a richer technical intelligence workspace. The key risk is building too much infrastructure before validating the daily-use loop.

Roadmap principle:

> First prove that users want to open tildewire every day. Then add more sources, smarter ranking, and collaboration features.

## 2. Release Themes

| Release | Theme | Main Question |
|---|---|---|
| v0.1 | Daily radar MVP | Can users scan useful technical signals quickly? |
| v0.2 | Source and detail expansion | Do richer details make users stay and save more? |
| v0.3 | Personalization and observability | Can tildewire reduce noise for repeat users? |
| v0.4 | Distribution and team workflows | Is there value beyond individual local use? |

## 3. v0.1: Daily Radar MVP

Goal:

- Deliver a fast, reliable, local-first TUI for daily technical scanning.

Sources:

- Hacker News.
- GitHub Trending.
- Hugging Face Papers.

Core features:

- Full-screen Bubble Tea TUI.
- Cached startup.
- Background refresh.
- Three-column layout.
- All view and source views.
- Source-native rank.
- Simple All Hot sort.
- Strong-key dedupe.
- Search.
- Save.
- Read/unread.
- Hide.
- Open URL.
- Copy URL.
- SQLite persistence.
- Stale cache fallback.
- Per-source rate limiting.

Engineering milestones:

1. Initialize project from `bubbletea-app-template`.
2. Add domain models.
3. Add SQLite schema and migrations.
4. Add store layer with sqlc.
5. Add HTTP client, cache, and rate limiter.
6. Implement HN adapter.
7. Build initial TUI with cached feed.
8. Implement GitHub Trending adapter.
9. Implement HF Papers adapter.
10. Add strong-key dedupe.
11. Add simple All Hot scoring.
12. Add saved/read/hidden state.
13. Add search and filter.
14. Add source status and stale cache fallback.
15. Add fixture and store tests.
16. Configure release build.

Exit criteria:

- `tildewire` opens quickly and works without tokens.
- Three MVP sources display usable data.
- Cached startup works offline.
- One source failure does not break the app.
- State persists across restarts.
- `go test ./...` passes.

## 4. v0.2: Details and Source Expansion

Goal:

- Make saved and detail workflows more useful.

Candidate features:

- Lobsters source.
- Product Hunt as optional source.
- HN top-level comments on detail view.
- Lobsters comments on detail view.
- GitHub README preview on detail view.
- HF paper detail expansion.
- Export saved items to Markdown, JSON, and CSV.
- Command palette.
- Filter modal refinements.
- VHS demo recording.

Product Hunt requirements:

- Must be disabled by default unless token is configured.
- Must show clear AUTH_REQUIRED state.
- Must preserve Product Hunt attribution.
- Must not be positioned as commercial usage without permission.

Exit criteria:

- At least one new source works without degrading v0.1 performance.
- Detail view improves save/open workflow.
- Export saved is reliable.
- Optional-token sources degrade cleanly.

## 5. v0.3: Personalization and Observability

Goal:

- Reduce repeat-user noise and make source health transparent.

Candidate features:

- FTS5 full-text search.
- Preference-based scoring from saved/hidden behavior.
- Boost and mute keywords.
- Boost languages and tags.
- Hidden rules by domain, keyword, source, tag, repo, and author.
- Source health dashboard.
- Fetch history view.
- SimHash-assisted fuzzy dedupe.
- Dedupe debug view with manual ignore for incorrect candidates.

Exit criteria:

- Repeat users see less unwanted content.
- Advanced dedupe does not silently destroy source context.
- Source health is understandable without logs.

## 6. v0.4: Remote and Team Capabilities

Goal:

- Explore whether tildewire can support shared technical awareness.

Candidate features:

- SSH-accessible TUI through Wish.
- Shared read-only dashboard.
- Team watchlists.
- Team saved state.
- Internal technology radar.
- Organization-level source config.

Exit criteria:

- Team use case is validated before adding permissions complexity.
- Individual local workflow remains fast and simple.

## 7. Deferred Ideas

These should not be started until the core workflow is proven:

- Local LLM summaries.
- Semantic embeddings.
- Browser extension.
- Mobile client.
- Hosted SaaS.
- Multi-user permission model.
- Complex Cobra/Kong command tree.

## 8. Decision Gates

Before v0.2:

- Are users opening tildewire repeatedly?
- Which source produces the most saved items?
- Is All Hot ranking good enough?
- Is the three-column layout comfortable in common terminal sizes?

Before v0.3:

- Are users hiding enough items to justify preference learning?
- Are duplicate items frequent enough to justify SimHash?
- Is search a primary workflow or secondary utility?

Before v0.4:

- Is there real team demand?
- Do users want shared state or just exportable artifacts?
- Would remote mode compromise the simplicity of the local app?

## 9. Recommended Build Order

```text
1. Domain model and SQLite migration
2. Store/sqlc and item state
3. HTTP client, raw cache, rate limiter
4. HN adapter
5. Minimal TUI rendering HN cached feed
6. GitHub Trending adapter
7. Hugging Face Papers adapter
8. Source-native sorting
9. Strong-key dedupe
10. All Hot scoring
11. Save/read/hide state
12. Search/filter
13. Stale cache fallback and source status
14. Fixture tests and store tests
15. Release packaging
```

## 10. Naming Decision

Use `tildewire` consistently across the product, repository, canonical binary, configuration paths, release artifacts, and documentation.
