# AGENTS.md

Repo guidance for coding agents working on tildewire.

## Core Rules

- tildewire is a Go terminal TUI. Keep product workflows inside the TUI, not new CLI subcommands, unless the user explicitly changes CLI scope.
- Supported command shape: `tildewire [--config path] [--debug] [--version] [--help]`.
- Current sources are GitHub Trending, Hacker News, AI Labs, Hugging Face Papers, Lobsters, and optional token-gated Product Hunt.
- Use `README.md` for current user-facing behavior, and use `docs/PRD.md`, `docs/TECHNICAL_DESIGN.md`, `docs/ROADMAP.md`, and `docs/RELEASE_NOTES.md` as product and architecture references.

## Boundaries

- `internal/boot`: flags.
- `internal/launcher`: shared startup wiring for the canonical binary.
- `internal/config`: config, paths, env overrides, persistence.
- `internal/domain`: shared source, item, detail, filter, and personalization types.
- `internal/sources`: source fetch and normalization only.
- `internal/httpx`: HTTP retry, cache, and rate limiting.
- `internal/normalize`: URL, repository, and arXiv canonicalization helpers.
- `internal/score` and `internal/dedupe`: ranking and similarity algorithms.
- `internal/app`: feed loading, refresh orchestration, filtering, scoring, item state, personalization, dedupe, and export.
- `internal/store`: SQLite, embedded migrations, transactions, sqlc queries.
- `internal/tui`: Bubble Tea model, views, key handling, settings, palette, browser/clipboard commands, Markdown rendering, and terminal image preview coordination.

Do not perform blocking I/O directly in Bubble Tea `Update()` methods. Route I/O through commands and application services.

Keep source visibility changes global: hidden sources must be omitted from refreshes, cached All-view entries, source counts, source health, recent fetch history, source commands, filters, and settings-driven runtime config.

Preview should stay fast and cache-backed from the current `domain.FeedEntry`. Lazy detail enrichment for comments, GitHub READMEs, paper metadata, Markdown image previews, and similar network-backed work belongs in detail commands and application services.

## Generated Code

- Do not edit `internal/store/generated` by hand.
- After changing `internal/store/queries.sql`, `internal/store/schema.sql`, or `sqlc.yaml`, run `go tool sqlc generate`.
- Keep `internal/store/migrations`, `internal/store/schema.sql`, `internal/store/queries.sql`, and generated code aligned.

## Documentation

- Keep `README.md` English-first and grounded in shipped behavior, not roadmap intent.
- Keep `AGENTS.md` compact and limited to repo rules, boundaries, generated-code rules, and verification.

## Verification

Run the narrowest meaningful checks before reporting completion:

```bash
go test ./...
go build -o tildewire .
go run . --help
scripts/install-local.sh /private/tmp/tildewire-install-check
```
