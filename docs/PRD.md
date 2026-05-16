# tildewire Product Requirements

> Version: v0.1 draft
> Date: 2026-05-10
> Product shape: terminal-first TUI
> Working title: tildewire

## 1. Product Summary

tildewire is a terminal-based daily technical signal radar for developers. It aggregates high-signal engineering, open source, and AI research content into one fast TUI, so users can open the terminal, scan the day, save useful items, hide noise, and return later offline.

The first product goal is not to recreate source websites. The goal is to help a developer answer one question quickly:

> What technical projects, discussions, and papers are worth my attention today?

## 2. Target Users

Primary users:

- Software engineers who already live in the terminal.
- Engineering leads and architects tracking technical trends.
- Independent developers watching open source, AI tools, and product ideas.
- AI/open-source practitioners following GitHub projects and papers.

Secondary users:

- Newsletter writers and technical curators.
- Developer advocates and product engineers monitoring ecosystem movement.

## 3. Core User Problems

1. High-value technical content is scattered across multiple sites.
2. The same project or paper may appear across GitHub, Hacker News, and AI sources, but the context is fragmented.
3. Existing feeds are noisy, browser-heavy, and easy to lose.
4. Developers want fast keyboard-first scanning, saving, filtering, and offline revisit.

## 4. Product Positioning

tildewire should feel like:

- `lazygit` for daily technical trends.
- A local-first feed reader specialized for developer signals.
- A compact terminal dashboard, not a generic news app.

tildewire should not become:

- A full social client.
- A browser replacement.
- A multi-command CLI platform before the TUI experience is proven.

## 5. Product Principles

1. Open fast, show cached content first.
2. Preserve source context instead of flattening everything into generic news.
3. Prefer fewer reliable sources over many fragile integrations.
4. Keep all core actions inside the TUI.
5. Degrade gracefully when one source fails.
6. Make saved, read, and hidden state durable and predictable.

## 6. MVP Scope

v0.1 should validate the core loop with three sources:

- Hacker News: broad technical discussion signal.
- GitHub Trending: open-source project signal.
- Hugging Face Papers: AI research signal.

MVP features:

- Launch `tildewire` directly into a full-screen TUI.
- Read cached content immediately on startup.
- Refresh supported sources in the background.
- Show a three-column interface: sources, feed list, preview/detail.
- Support source views and an All view.
- Preserve source-native rank in single-source views.
- Provide a simple All Hot sort across sources.
- Search title, summary, tags, repo, and paper metadata.
- Save and unsave items.
- Mark items read or unread.
- Hide items.
- Open item URL or source discussion URL.
- Copy URL or Markdown link.
- Persist local cache and item state in SQLite.
- Support offline reading from cache.
- Show source health in the status bar.
- Respect rate limits and use stale cache on failure.

## 7. Non-MVP Scope

Not included in v0.1:

- Product Hunt as a required source.
- Lobsters as a required source.
- Account sync or multi-device sync.
- Local LLM summaries.
- Full comment tree preload.
- Team or remote dashboard.
- Multi-user permissions.
- Fuzzy SimHash dedupe as default behavior.
- Full Cobra/Kong multi-command CLI.
- Commercial Product Hunt usage.

Product Hunt may be introduced later as an optional source because it requires an access token and has explicit commercial-use restrictions.

## 8. User Journeys

### 8.1 First Launch

1. User runs `tildewire`.
2. App creates config and local database if missing.
3. App opens the TUI immediately.
4. If no cache exists, the feed area shows source loading states.
5. HN, GitHub Trending, and HF Papers refresh in the background.
6. Any source failure is shown in the status bar, without blocking the app.

### 8.2 Daily Scan

1. User runs `tildewire`.
2. Cached items render in under 300 ms.
3. User scans All Hot view.
4. User opens interesting items with `Enter`.
5. User saves useful items with `s`.
6. User hides irrelevant items with `h`.
7. User opens original pages with `o`.
8. User exits with `q`.

### 8.3 Offline Use

1. User starts tildewire without network.
2. App loads cached items.
3. Status bar marks sources as stale or network error.
4. Read, saved, and hidden actions continue to work locally.

### 8.4 Source Failure

1. One source returns 429, parser error, or network timeout.
2. That source enters a visible degraded state.
3. Other sources continue refreshing.
4. Old cached items from the failed source remain visible if available.

## 9. Main TUI Requirements

Default layout:

```text
┌─ tildewire ───────────────────────────────────────────────┐
│ View: All  Sort: Hot  Filter: unread        / Search     │
├───────────────┬─────────────────────────────┬────────────┤
│ SOURCES       │ FEED                        │ PREVIEW    │
│ ● All      82 │ 1 [GH] owner/repo           │ title      │
│ ● GitHub   25 │   +812 stars today          │ summary    │
│ ● HN       42 │ 2 [HN] SQLite discussion    │ actions    │
│ ● HF       15 │   421 pts · 88 comments     │ status     │
├───────────────┴─────────────────────────────┴────────────┤
│ j/k move  Enter detail  / search  s save  h hide  q quit │
└───────────────────────────────────────────────────────────┘
```

Required shortcuts:

| Shortcut | Action |
|---|---|
| `j` / `Down` | Next item |
| `k` / `Up` | Previous item |
| `Enter` | Detail view |
| `Esc` | Back |
| `q` | Quit |
| `a` | All view |
| `1` | GitHub |
| `2` | Hacker News |
| `3` | Hugging Face Papers |
| `/` | Search |
| `f` | Filter |
| `r` | Refresh |
| `s` | Save or unsave |
| `m` | Mark read |
| `u` | Mark unread |
| `h` | Hide |
| `o` | Open original URL |
| `O` | Open source discussion URL |
| `y` | Copy URL |
| `Y` | Copy Markdown link |
| `?` | Help |

## 10. Source Requirements

### 10.1 Hacker News

Required views:

- Top
- Best
- New
- Show HN

Required fields:

- Title
- URL
- Author
- Score
- Comment count
- Published time
- HN item id
- Source rank
- Discussion URL

### 10.2 GitHub Trending

Required views:

- Daily
- Weekly
- Language filter, initially Go, Rust, Python, TypeScript.

Required fields:

- Owner/repo
- Description
- Language
- Stars
- Forks
- Stars today or period stars where available
- Source rank
- Repository URL

### 10.3 Hugging Face Papers

Required view:

- Daily papers

Required fields:

- Paper id or arXiv id where available
- Title
- Summary
- Authors or organization where available
- Upvotes
- Comment count
- GitHub repo reference where available
- Source rank
- Paper URL

## 11. Data State Requirements

Each item must support:

- Read/unread.
- Saved/unsaved.
- Hidden.
- First seen time.
- Last seen time.
- Source badges.
- Source metrics.

State must survive app restart.

## 12. Sorting and Filtering Requirements

Single-source views:

- Default to source-native rank.

All view:

- Default to Hot.
- Hot v0.1 can use a simple formula combining source rank, recency, and source count.

Required filters:

- Source.
- Saved.
- Unread.
- Language.
- Tag.
- Text search.

## 13. Success Metrics

Product metrics:

- User can complete a daily scan in under 5 minutes.
- Cached startup renders in under 300 ms on a warm database.
- At least one source failure does not prevent browsing other sources.
- Saved and hidden state remains correct after restart.
- A new user can use the app without any token.

Engineering metrics:

- `go test ./...` passes.
- Adapter fixture tests exist for all MVP sources.
- SQLite migrations work from an empty database.
- Parser failures do not crash the TUI.
- Release builds produce macOS, Linux, and Windows binaries.

## 14. Acceptance Criteria

v0.1 is acceptable when:

- Running `tildewire` opens the TUI.
- HN, GitHub Trending, and HF Papers can fetch and display items.
- All view displays merged feed items with source badges.
- Single-source views preserve source-native ordering.
- Search works across title, summary, tags, repo, and paper metadata.
- Save, read, and hidden state are persisted.
- App works from cache when offline.
- 429 or network failures show source status and stale data.
- User can open and copy item links.

## 15. Open Product Questions

- Should the final product name be tildewire or tildewire?
- Should Product Hunt be optional in v0.2 or omitted until commercial-use constraints are resolved?
- Should Lobsters ship before or after Product Hunt?
- Should v0.1 include export saved Markdown, or move it to v0.2?
- Should the first release prefer a compact terminal UI or a richer detail-first layout?
