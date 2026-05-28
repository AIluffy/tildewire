package app

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AIluffy/tildewire/internal/domain"
)

// ExportFormat identifies a saved-items export format.
type ExportFormat string

const (
	// ExportMarkdown writes saved items as Markdown.
	ExportMarkdown ExportFormat = "markdown"
	// ExportJSON writes saved items as JSON.
	ExportJSON ExportFormat = "json"
	// ExportCSV writes saved items as CSV.
	ExportCSV ExportFormat = "csv"
)

// ExportOptions controls saved-items export.
type ExportOptions struct {
	Format ExportFormat
	Dir    string
	Now    time.Time
}

// ExportResult describes a completed saved-items export.
type ExportResult struct {
	Path   string
	Count  int
	Format ExportFormat
}

// ExportSaved writes all saved items, including hidden saved items, to disk.
func (s *Service) ExportSaved(ctx context.Context, options ExportOptions) (ExportResult, error) {
	format := normalizeExportFormat(options.Format)
	dir := strings.TrimSpace(options.Dir)
	if dir == "" {
		return ExportResult{}, fmt.Errorf("export dir is required")
	}
	now := options.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	entries, err := s.feeds.ListFeed(ctx, domain.FeedQuery{SavedOnly: true, IncludeHidden: true, Limit: 10000})
	if err != nil {
		return ExportResult{}, err
	}
	applySort(entries, domain.SourceAll, now, nil, domain.PreferenceProfile{})
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ExportResult{}, err
	}
	path := filepath.Join(dir, exportFilename(format, now))
	data, err := renderExport(format, entries, now)
	if err != nil {
		return ExportResult{}, err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return ExportResult{}, err
	}
	return ExportResult{Path: path, Count: len(entries), Format: format}, nil
}

func normalizeExportFormat(format ExportFormat) ExportFormat {
	switch format {
	case ExportJSON, ExportCSV:
		return format
	default:
		return ExportMarkdown
	}
}

func exportFilename(format ExportFormat, now time.Time) string {
	ext := "md"
	if format == ExportJSON {
		ext = "json"
	}
	if format == ExportCSV {
		ext = "csv"
	}
	return fmt.Sprintf("tildewire-saved-%s.%s", now.UTC().Format("20060102-150405"), ext)
}

func renderExport(format ExportFormat, entries []domain.FeedEntry, exportedAt time.Time) ([]byte, error) {
	switch format {
	case ExportJSON:
		return renderJSONExport(entries, exportedAt)
	case ExportCSV:
		return renderCSVExport(entries)
	default:
		return renderMarkdownExport(entries, exportedAt), nil
	}
}

func renderMarkdownExport(entries []domain.FeedEntry, exportedAt time.Time) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# tildewire saved items\n\n")
	fmt.Fprintf(&b, "Exported: %s\n\n", exportedAt.UTC().Format(time.RFC3339))
	for _, entry := range entries {
		url := entry.Item.URL
		if url == "" {
			url = entry.Item.CommentsURL
		}
		if url != "" {
			fmt.Fprintf(&b, "- [%s](%s)", entry.Item.Title, url)
		} else {
			fmt.Fprintf(&b, "- %s", entry.Item.Title)
		}
		if entry.Item.Subtitle != "" {
			fmt.Fprintf(&b, " - %s", entry.Item.Subtitle)
		}
		if entry.State.Hidden {
			b.WriteString(" - hidden")
		}
		b.WriteString("\n")
		if len(entry.Item.Tags) > 0 {
			fmt.Fprintf(&b, "  - Tags: %s\n", strings.Join(entry.Item.Tags, ", "))
		}
		if source := entry.PrimarySource().Source; source != "" {
			fmt.Fprintf(&b, "  - Source: %s\n", source)
		}
	}
	return []byte(b.String())
}

type exportDocument struct {
	ExportedAt string       `json:"exported_at"`
	Count      int          `json:"count"`
	Items      []exportItem `json:"items"`
}

type exportItem struct {
	Title     string   `json:"title"`
	URL       string   `json:"url,omitempty"`
	Source    string   `json:"source,omitempty"`
	Subtitle  string   `json:"subtitle,omitempty"`
	Summary   string   `json:"summary,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	SavedAt   string   `json:"saved_at,omitempty"`
	Hidden    bool     `json:"hidden,omitempty"`
	Canonical string   `json:"canonical_key,omitempty"`
}

func renderJSONExport(entries []domain.FeedEntry, exportedAt time.Time) ([]byte, error) {
	doc := exportDocument{
		ExportedAt: exportedAt.UTC().Format(time.RFC3339),
		Count:      len(entries),
		Items:      make([]exportItem, 0, len(entries)),
	}
	for _, entry := range entries {
		doc.Items = append(doc.Items, exportItemFromEntry(entry))
	}
	return json.MarshalIndent(doc, "", "  ")
}

func renderCSVExport(entries []domain.FeedEntry) ([]byte, error) {
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)
	if err := writer.Write([]string{"title", "url", "source", "subtitle", "saved_at", "hidden", "tags"}); err != nil {
		return nil, err
	}
	for _, entry := range entries {
		item := exportItemFromEntry(entry)
		if err := writer.Write([]string{
			item.Title,
			item.URL,
			item.Source,
			item.Subtitle,
			item.SavedAt,
			fmt.Sprintf("%t", item.Hidden),
			strings.Join(item.Tags, " "),
		}); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func exportItemFromEntry(entry domain.FeedEntry) exportItem {
	savedAt := ""
	if entry.State.SavedAt != nil {
		savedAt = entry.State.SavedAt.UTC().Format(time.RFC3339)
	}
	url := entry.Item.URL
	if url == "" {
		url = entry.Item.CommentsURL
	}
	return exportItem{
		Title:     entry.Item.Title,
		URL:       url,
		Source:    string(entry.PrimarySource().Source),
		Subtitle:  entry.Item.Subtitle,
		Summary:   entry.Item.Summary,
		Tags:      entry.Item.Tags,
		SavedAt:   savedAt,
		Hidden:    entry.State.Hidden,
		Canonical: entry.Item.CanonicalKey,
	}
}
