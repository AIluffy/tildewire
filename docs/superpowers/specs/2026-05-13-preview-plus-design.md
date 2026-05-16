# Preview+ Design

## Context

tildewire currently has two item-reading surfaces. The right-side preview panel is instant and cache-backed, but only shows the selected item's title, author, summary, item URL, and source discussion URL. The full detail view loads richer source-native content asynchronously after `Enter`, including Hacker News comments, Lobsters comments, GitHub README previews, and Hugging Face paper metadata.

Preview+ strengthens the right-side panel without changing the detail workflow. The goal is to help users decide faster during feed scanning by showing heat signals and content cues that already exist on the selected `domain.FeedEntry`.

## Goals

- Make the preview panel useful for quick triage in the main three-column TUI.
- Prioritize heat signals and content cues over action hints.
- Keep preview rendering instant, offline-friendly, and safe during rapid `j` / `k` navigation.
- Preserve the existing fixed-height, scrollable preview behavior.

## Non-Goals

- Do not call `LoadDetail()` from the preview panel.
- Do not fetch comments, README content, paper metadata, or Product Hunt detail content for preview.
- Do not add store migrations or persistent detail caching in this slice.
- Do not replace the full detail view.

## Layout

Preview+ uses the Compact Headline layout with Source-Native First metrics:

1. Header row: `PREVIEW` plus the selected item's primary source badge, such as `[GH]`.
2. Title row: the selected item's title, clipped only when needed for terminal width.
3. Source-native metrics row:
   - GitHub: source rank/view, stars, stars today, forks.
   - Hacker News: source rank/view, points, comments.
   - Hugging Face Papers: source rank/view, paper id or arXiv id, repo/project hint, tags.
   - Lobsters: source rank/view, score, comments, tags.
   - Product Hunt: source rank/view, votes, comments, makers or topics.
4. Summary block from `FeedItem.Summary`.
5. Content cue lines for language, tags, author, organization, and item type.
6. Link lines for item URL and source discussion URL.

The first screen should answer two questions quickly: why is this item hot, and what is it about?

## Data Flow

Preview+ reads only from the selected `domain.FeedEntry`.

- `entry.PrimarySource()` determines the badge, source view, source rank, and primary source metrics.
- Metric values prefer `entry.PrimarySource().Metrics`.
- Missing primary-source metrics fall back to `entry.Item.Metrics`.
- Source labels and badges use the existing source catalog in `internal/app`.
- URL lines use `entry.Item.URL` and `entry.Item.CommentsURL`.

No preview path performs blocking I/O directly in Bubble Tea `Update()` methods.

## Degradation

Preview+ must degrade gracefully when fields are missing.

- Do not render zero-value metrics unless the source actually provided them.
- Only render rank when `SourceRank > 0`.
- Skip empty tags, language, author, organization, item type, URL, and discussion URL lines.
- If no metrics are available, the preview still renders badge, title, summary, cues, and links.
- Width and height behavior continues to use the existing clipping, wrapping, fill, and scroll helpers.

## Implementation Shape

Keep the change focused in `internal/tui`.

- `renderPreview()` remains responsible for fixed height and scroll offset handling.
- `previewContentLines()` becomes the high-level row assembler.
- Add small helpers for preview header, source-native metrics, content cues, and links.
- Keep source-specific metric mapping isolated in a helper so the preview renderer stays readable.

The implementation should not modify source adapters, detail adapters, store schema, or app refresh orchestration.

## Testing

Add focused TUI tests that cover:

- Compact preview renders source badge, title, source-native metrics, summary, and content cues.
- Missing metrics degrade without rendering misleading zeroes.
- Preview height and scroll behavior remain fixed.
- Existing selection-change reset behavior still works.
- At least one test covers non-GitHub metrics, such as Hacker News or Lobsters.

Run the narrow repo checks before implementation completion:

```bash
go test ./...
go build -o tildewire .
go run . --help
```
