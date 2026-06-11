# tildewire

Terminal daily technical signal radar for developers.

tildewire is a local-first terminal TUI for scanning high-signal developer content. It pulls Hacker News, GitHub Trending, AI Labs news, Hugging Face Papers, Lobsters, and optional Product Hunt into one keyboard-first interface with cached startup, background refresh progress, local state, source health, search, filtering, personalization, Markdown-rich detail views, and saved-item export.

The product is intentionally TUI-first. The supported command shape stays small:

```text
tildewire [--config path] [--debug] [--version] [--help]
```

`tildewire` is the canonical product and binary name.

There are no `fetch`, `list`, `export`, `cache`, or `config` subcommands. Scanning, refresh, filtering, saving, hiding, export, source health, and cache management all live inside the terminal UI.

## Installation

### Homebrew

Install the canonical binary on macOS or Linux with the tildewire tap:

```bash
brew install --cask AIluffy/tap/tildewire
```

The Homebrew cask installs `tildewire`.

### Install Script

For macOS and Linux, install the latest GitHub Release binary:

```bash
curl -fsSL https://raw.githubusercontent.com/AIluffy/tildewire/main/scripts/install.sh | sh
```

Install a specific release:

```bash
curl -fsSL https://raw.githubusercontent.com/AIluffy/tildewire/main/scripts/install.sh | TILDEWIRE_VERSION=v0.4.2 sh
```

The installer downloads the matching release archive, verifies it against `checksums.txt`, and installs `tildewire` to `${TILDEWIRE_BIN_DIR:-$HOME/.local/bin}`.

### Go

Install the canonical binary with Go:

```bash
go install github.com/AIluffy/tildewire@latest
```

This installs `tildewire`.

### Release Assets

GitHub Releases provide:

- macOS, Linux, and Windows archives for amd64 and arm64.
- Linux `.deb`, `.rpm`, and `.apk` packages.
- `checksums.txt` for release verification.

Download binaries and packages from the [releases](https://github.com/AIluffy/tildewire/releases) page.

### Build From Source

```bash
git clone https://github.com/AIluffy/tildewire.git
cd tildewire
go build -o tildewire .
```

## What It Does

- Shows cached items immediately, then refreshes sources in the background.
- Merges multiple developer-signal sources into an All view, maintains a personalized Recommend view, and preserves source-native ranking in source views.
- Lets you scan with a three-panel layout: Sources, Feed, and Preview.
- Opens richer detail views for source-native context such as comments, GitHub READMEs, and paper metadata.
- Renders GitHub README Markdown with tables, visual dividers, padded code blocks, click-to-copy code headers, and optional terminal image previews.
- Persists saved, read, hidden, source health, fetch history, personalization rules, recommendation scores, fuzzy dedupe candidates, and raw HTTP cache in local SQLite.
- Supports full-text search, saved/unread filters, source and scope filters, language and tag facets, and hidden-item inclusion.
- Exports saved items to Markdown, JSON, or CSV.
- Works offline from cache, with local item actions still available.

## Daily Workflow

1. Start tildewire with `go run .` during development, or `tildewire` after building/installing.
2. Cached feed data appears first so the TUI is useful before network refresh completes.
3. Background refresh updates enabled sources and source health without blocking the UI.
4. Use the Sources panel or shortcuts to choose All, Recommend, GitHub, Hacker News, AI Labs, Hugging Face Papers, Lobsters, or Product Hunt.
5. Scan the Feed panel and use Preview+ to triage the selected item with source badges, ranks, source-native metrics, summary text, content cues, and links.
6. Press `Enter` when an item needs deeper context. Detail loading is lazy and asynchronous.
7. Save useful items with `s`, mark read/unread with `m` or `u`, hide noise with `h`, open URLs with `o` or `O`, and copy links with `y` or `Y`.
8. Use `/`, `f`, `p`, and `!` for search, filters, command palette actions, and source health.
9. Export saved items from the command palette when you want a reusable reading list.

## Sources

| Source | Built-in scopes | Auth | Detail enrichment |
|---|---|---|---|
| GitHub Trending | Today, this week, this month, and common daily language scopes such as Go, Rust, Python, and TypeScript. GitHub scope parsing also supports programming language and spoken language filters. | Optional `github_token` or `GITHUB_TOKEN` helps with README preview rate limits. | README preview rendered in the detail view, including tables, visual dividers, code-block copy, and optional terminal image previews. |
| Hacker News | Top, Best, New, Show HN. | None. | Top-level comments in the detail view. |
| AI Labs | OpenAI News, Anthropic News, Google DeepMind News, Meta AI Blog. | None. | Feed display only; source URLs open the official lab post. |
| Hugging Face Papers | Daily papers. | None. | Paper metadata and paper-related sections when available. |
| Lobsters | Hottest, Newest. | None. | Top-level comments in the detail view. |
| Product Hunt | Today, Weekly. | Required for refresh through `product_hunt_token` or `PRODUCT_HUNT_TOKEN`. | Feed display only; source URLs preserve Product Hunt attribution. |

Product Hunt is optional and token-gated. It can appear in the TUI by default, but refresh is skipped and source health reports `AUTH_REQUIRED` until a Product Hunt token is configured. tildewire uses Product Hunt for read-only feed display, preserves Product Hunt post URLs, and does not position the integration for commercial use without Product Hunt permission.

## TUI Surfaces

### Sources

The Sources panel contains All, Recommend, and every enabled source in the configured order. Recommend is a virtual view, not a refresh adapter or configurable source. Source counts reflect the latest refreshed upstream windows stored locally, not accumulated historical cache totals. Use `Left` and `Right` to move focus between Sources, Feed, and Preview. When Sources is focused:

- `j` / `Down` and `k` / `Up` switch the active source.
- `J` / `Shift+Down` and `K` / `Shift+Up` reorder focused sources and save the order to config.
- Number shortcuts jump directly to a source from the main view.

### Feed

The Feed panel lists cached and refreshed entries for the current view. All view uses a hot score with source metrics, recency, explicit rules, and implicit saved/hidden preference signals. Recommend shows up to 10 top persisted recommendation scores from the latest cached feed window, built from boost rules and saved-item preference signals, reduced by mute rules and hidden-item preference signals. Single-source views preserve source-native rank for that source scope.

Each feed row shows a compact source badge, title, and subtitle. Hidden items stay out of the normal feed unless the filter includes hidden items.

### Preview+

Preview is the right-side triage surface. It is intentionally fast and cache-backed: it reads only the selected `FeedEntry` and does not fetch comments, READMEs, or paper details.

Preview shows:

- Source badge and source-native rank/scope.
- Metrics such as stars, forks, stars today, points, upvotes, comments, votes, paper IDs, and repository references when available.
- Summary text.
- Content cues such as language, item type, author, organization, and tags.
- Item URL and source discussion URL.

Use `PageUp` / `Ctrl+U` and `PageDown` / `Ctrl+D` to scroll Preview when content is taller than the panel.

### Detail

`Enter` opens a full detail view for the selected item. Detail loading is lazy and runs through Bubble Tea commands, so blocking I/O does not happen directly inside `Update()`.

Detail can include:

- The item title, subtitle, summary, tags, language, author, item URL, and source URL.
- GitHub README content rendered with Glamour, with table handling, visual dividers, padded syntax-highlighted code blocks, click-to-copy code headers, and optional terminal image previews.
- Hacker News or Lobsters top-level comments.
- Hugging Face paper metadata and sections.
- Provider labels so it is clear which source supplied detail content.

Use `Esc` to return, `j` / `k`, mouse wheel, `PageUp` / `Ctrl+U`, and `PageDown` / `Ctrl+D` to scroll. Click a rendered code block's copy icon to copy the underlying code text. The same save/read/hide/open/copy item actions work where applicable.

### Search

Press `/` to enter search mode. Search is backed by a local SQLite FTS5 table and covers title, subtitle, summary, author, organization, language, repo references, arXiv or paper IDs, metadata, and tags. The current search query becomes part of the active feed filter and can also seed personalization rules from the command palette.

### Filter

Press `f` to open the filter modal. It can adjust:

- Source.
- Source scope.
- Saved-only.
- Unread-only.
- Language.
- Tag.
- Show hidden items alongside the normal feed.

The language and tag options are drawn from current feed data, with common developer language values included. Saved and unread filters can be combined. To recover hidden items, enable Show hidden items, select a hidden row, then press `h` or run the Restore item palette action.

### Command Palette

Press `p` to open the command palette. Type to filter commands, move with `j` / `k`, run with `Enter`, or cancel with `Esc`.

Palette actions include:

- Export saved Markdown, JSON, or CSV.
- Refresh sources.
- Clear cache.
- Show hidden items in the current feed.
- Open or copy the selected item URL.
- Save or unsave the selected item.
- Mark the selected item read.
- Hide the selected item, or restore it when the selected item is hidden.
- Create boost, mute, or hide rules from the current item or current search.
- Open Personalization Rules.
- Open Dedupe Candidates.
- Switch views.
- Open Filter, Settings, or Source Health.

### Source Health

Press `!` to open Source Health. It shows current status for enabled sources and recent fetch history, including source view, status, duration, item count, stale reason, errors, and timestamps. Press `r` from this panel to retry a failed refresh.

Statuses include `UNKNOWN`, `OK`, `STALE`, `REFRESHING`, `RATE_LIMITED`, `AUTH_REQUIRED`, `PARSER_BROKEN`, `NETWORK_ERROR`, and `DISABLED`.

### Settings

Press `c` to open Settings. The settings form can edit:

- Theme: catppuccin, dracula, gruvbox, nord, tokyo-night, solarized-dark, one-dark, everforest, rose-pine, or monokai.
- Markdown image preview backend: auto, off, kitty, iterm, sixel, or halfblocks.
- Raw HTTP cache TTL in hours.
- Accessible form mode.
- Visible sources.
- GitHub token.
- Product Hunt token.

Settings are saved back to `config.toml`. Source visibility and token changes are applied to the running service without requiring a process restart. Disabling a source removes it from source navigation, filters, palette source commands, source health, recent fetch history, source counts, refresh execution, and future Recommend scoring while preserving local cached data.

Theme commands are also available from the command palette as `Theme: ...` entries. Themes affect the TUI chrome, source badges, status colors, command/settings overlays, and Markdown detail accents while preserving the terminal background.

## Personalization and Dedupe

tildewire combines explicit rules and local behavior signals in the All and Recommend views.

Explicit rules:

- Boost matching items.
- Mute matching items.
- Hide matching items.
- Match targets include keyword, language, tag, domain, source, repo, and author.

Implicit All-view signals:

- Saved tags, languages, repos, authors, and sources slightly increase related All-view ranking.
- Hidden tags, languages, repos, authors, and sources slightly reduce related All-view ranking.

Recommend is trained by explicit local interactions: save/unsave, hide/restore, read/unread, opening detail, opening item or source URLs, and copying URLs or Markdown links. It uses time-decayed item terms from titles, summaries, tags, languages, repos, authors, organizations, and source context. Recommend shows only the top 10 recommendations from the latest cached feed window, stays empty until it has a positive interest signal, and always excludes durable hidden items and hide-rule matches, even when Show hidden items is enabled.

Open the Personalization Rules panel from the command palette to review rules. Use `Space` to toggle a rule and `d` to delete it.

Open `Recommend diagnostics` from the command palette to inspect the read-only recommendation profile, selected-item score breakdown, matched terms, reasons, and exclusion status. This panel is local-only and does not change recommendation scores or training history.

Fuzzy dedupe candidates are generated from item similarity and shown in the Dedupe Candidates panel. These are suggestions, not silent destructive merges. Use `i` to ignore a candidate you do not want to see again.

## Item Actions

| Action | Key | Notes |
|---|---|---|
| Save or unsave | `s` | Saved state persists across restarts and is included in exports. |
| Mark read | `m` | Read state persists locally. |
| Mark unread | `u` | Useful when revisiting cached items. |
| Hide or restore | `h` | Hidden items leave the normal feed. Use Show hidden items, then select a hidden item and press `h` to restore it. |
| Open item URL | `o` | Opens the primary item URL in the system browser. |
| Open source URL | `O` | Opens the source discussion or source-native URL when available. |
| Copy URL | `y` | Copies the primary item URL. |
| Copy Markdown link | `Y` | Copies `[title](url)` for the selected item. |
| Copy code block | code block copy icon | Detail view only; copies the underlying code text, not the rendered box. |

## Export and Cache Management

Saved-item exports are available from the command palette:

- Markdown: `tildewire-saved-YYYYMMDD-HHMMSS.md`
- JSON: `tildewire-saved-YYYYMMDD-HHMMSS.json`
- CSV: `tildewire-saved-YYYYMMDD-HHMMSS.csv`

Exports are written under the local data directory's `exports/` folder. Hidden saved items are included so exports preserve intentional saves even when those items are not visible in the default feed.

The Clear cache palette action clears refreshable cache data, source status state, rate-limit cooldowns, dedupe candidates, and unsaved cached items/search rows while preserving explicitly saved items. After clearing, tildewire immediately triggers a forced refresh of the visible scope.

## Keyboard Shortcuts

| Key | Action |
|---|---|
| `Left` | Focus the panel to the left |
| `Right` | Focus the panel to the right |
| `j` / `Down` | Move down in the focused panel |
| `k` / `Up` | Move up in the focused panel |
| `J` / `Shift+Down` | Move the focused source down in Sources order |
| `K` / `Shift+Up` | Move the focused source up in Sources order |
| `PageUp` / `Ctrl+U` | Scroll Preview or Detail up |
| `PageDown` / `Ctrl+D` | Scroll Preview or Detail down |
| `Enter` | Open detail view or run the selected modal command |
| `Esc` | Back or cancel the current modal |
| `q` / `Ctrl+C` | Quit |
| `a` | All sources |
| `0` | Recommend |
| `1` | GitHub Trending |
| `2` | Hacker News |
| `3` | Hugging Face Papers |
| `4` | Lobsters |
| `5` | Product Hunt |
| `6` | AI Labs |
| `v` | Cycle source scope for the active source |
| `/` | Search |
| `f` | Filter |
| `p` | Command palette |
| `c` | Settings in the main view |
| `r` | Refresh visible source scope |
| `s` | Save or unsave |
| `m` | Mark read |
| `u` | Mark unread |
| `h` | Hide item or restore selected hidden item |
| `o` | Open item URL |
| `O` | Open source URL |
| `y` | Copy URL |
| `Y` | Copy Markdown link |
| `!` | Source health |
| `?` | Toggle expanded help |

## Requirements

- Go 1.26.3 or newer compatible Go toolchain.
- A terminal that supports full-screen TUI applications.
- Network access for fresh source refreshes. Cached content and local item state continue to work offline.

## Run Locally

From the repository root:

```bash
go run .
```

First launch creates the default config file, local data directories, and SQLite database if they do not already exist. If the config file is newly created, tildewire opens the settings form first.

Useful startup commands:

```bash
go run . --help
go run . --version
go run . --debug
go run . --config /path/to/config.toml
```

Build and run local binaries:

```bash
go build -o tildewire .
./tildewire
```

Install from a local source checkout for daily terminal use:

```bash
scripts/install-local.sh
```

The installer writes `tildewire` to `${TILDEWIRE_BIN_DIR:-$HOME/.local/bin}`.

To install somewhere else:

```bash
TILDEWIRE_BIN_DIR=/usr/local/bin scripts/install-local.sh
```

## Configuration

Configuration precedence:

```text
CLI flag > environment variable > config.toml > default
```

Local files on macOS and Linux:

```text
~/.config/tildewire/config.toml
~/.local/share/tildewire/tildewire.db
~/.local/share/tildewire/exports/
~/.cache/tildewire/
~/.local/state/tildewire/debug.log
```

Local files on Windows:

```text
%APPDATA%\tildewire\config.toml
%LOCALAPPDATA%\tildewire\tildewire.db
%LOCALAPPDATA%\tildewire\exports\
%LOCALAPPDATA%\tildewire\cache\
%LOCALAPPDATA%\tildewire\state\
```

Supported environment variables:

```text
TILDEWIRE_CONFIG
TILDEWIRE_HTTP_TIMEOUT
TILDEWIRE_HTTP_CACHE_TTL_HOURS
TILDEWIRE_THEME
TILDEWIRE_GLAMOUR_STYLE
TILDEWIRE_MARKDOWN_IMAGE_PREVIEW
TILDEWIRE_ACCESSIBLE_FORMS
TILDEWIRE_DEBUG
GITHUB_TOKEN
PRODUCT_HUNT_TOKEN
```

User-editable TOML keys:

```toml
http_timeout_seconds = 12
http_cache_ttl_hours = 6
theme = "catppuccin"
glamour_style = "dark"
markdown_image_preview = "auto"
accessible_forms = false
debug = false

# Choose which Sources panel entries and refresh adapters are enabled.
enabled_sources = ["github", "hackernews", "ailabs", "huggingface", "lobsters", "producthunt"]

# Reorder the Sources panel.
source_order = ["github", "hackernews", "ailabs", "huggingface", "lobsters", "producthunt"]

# Optional tokens. Environment variables with the same purpose override these.
github_token = ""
product_hunt_token = ""
```

`theme` controls the global TUI palette and drives Markdown detail styling. `glamour_style` is still read for compatibility with older config files, but Settings writes the theme-derived Markdown style.

`markdown_image_preview = "auto"` renders non-badge GitHub README images as halfblocks inside the scrollable detail view so images move with the text layout. Set `markdown_image_preview = "kitty"`, `"iterm"`, or `"sixel"` to explicitly request terminal graphics, `"halfblocks"` to force text-cell image rendering, or `"off"` to keep text placeholders only. The legacy value `"chafa"` is normalized to `"halfblocks"` when read from older config files.

GitHub detail enrichment works without authentication, but unauthenticated REST API requests are limited by GitHub. Set `github_token` in `config.toml`, use the Settings screen, or set `GITHUB_TOKEN` when README previews frequently hit 403 or 429 rate limits.

Product Hunt refresh requires `product_hunt_token` in `config.toml`, the Settings screen, or `PRODUCT_HUNT_TOKEN` in the environment.

## Troubleshooting

| Symptom | What to check |
|---|---|
| Product Hunt shows `AUTH_REQUIRED` | Configure `product_hunt_token` or `PRODUCT_HUNT_TOKEN`, or disable Product Hunt in Settings. |
| GitHub README preview hits 403 or 429 | Configure `github_token` or `GITHUB_TOKEN`. |
| Feed says there are no cached items | Press `r` to refresh, check network access, then inspect `!` Source Health. |
| A source is stale or rate limited | Source Health shows the status, last success, stale reason, and recent fetch errors. Cached data remains available. |
| Search or filters hide too much | Open `f`, clear saved/unread/language/tag/show-hidden facets, or return to All view with `a`. |
| Need a fresh cache | Use `p`, run Clear cache, and wait for the automatic visible-scope refresh. Saved items are preserved. |
| Need debug logs | Start with `--debug` or set `TILDEWIRE_DEBUG=true`; logs are written under the local state directory. |

## Development

Run the test suite:

```bash
go test ./...
```

Build the binary:

```bash
go build -o tildewire .
```

Check the local installer:

```bash
scripts/install-local.sh /private/tmp/tildewire-install-check
```

Check the supported CLI shape:

```bash
go run . --help
```

Regenerate typed SQL after changing `internal/store/queries.sql`, `internal/store/schema.sql`, or `sqlc.yaml`:

```bash
go tool sqlc generate
```

Do not edit `internal/store/generated` by hand. The store embeds migrations from `internal/store/migrations`; keep migration SQL, schema SQL, query SQL, and generated code aligned when changing persistence behavior.

## Architecture Notes

tildewire is organized around a small set of internal boundaries:

- `internal/boot`: flag parsing and supported CLI shape.
- `internal/launcher`: shared startup wiring for the canonical and short binaries.
- `internal/config`: config loading, defaults, paths, env overrides, and persistence.
- `internal/domain`: shared source, item, detail, filter, and personalization types.
- `internal/sources`: source adapters and source-native normalization.
- `internal/httpx`: retry, raw HTTP cache, conditional requests, and rate limiting.
- `internal/normalize`: URL, repository, and arXiv canonicalization helpers.
- `internal/score` and `internal/dedupe`: ranking and similarity algorithms.
- `internal/app`: feed loading, refresh orchestration, filtering, scoring, item state, personalization, dedupe, and export.
- `internal/store`: SQLite, migrations, transactions, SQL queries, and sqlc-generated accessors.
- `internal/tui`: Bubble Tea model, rendering, key handling, forms, palette, settings, browser, clipboard commands, Markdown rendering, and terminal image preview coordination.

Bubble Tea `Update()` methods do not perform blocking I/O directly. I/O flows through commands and application services.

## Project Docs

- [Product requirements](docs/PRD.md)
- [Technical design](docs/TECHNICAL_DESIGN.md)
- [Roadmap](docs/ROADMAP.md)
- [Release notes](docs/RELEASE_NOTES.md)

## License

tildewire is released under the [MIT License](LICENSE).
