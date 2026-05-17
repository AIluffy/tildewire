package tui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/progress"
	"charm.land/lipgloss/v2"

	"github.com/AIluffy/tildewire/internal/app"
	"github.com/AIluffy/tildewire/internal/domain"
)

func (m Model) loadingLines(width int) []string {
	statuses := m.visibleStatuses()
	if m.view != domain.SourceAll {
		return m.singleSourceLoadingLines(width, m.view, statuses, m.loadingSpinner.View())
	}
	lines := []string{m.styles.muted.Render("Loading sources...")}
	if bar := m.loadingProgressBar(width, statuses); bar != "" {
		lines = append(lines, bar)
	}
	return append(lines, m.loadingSourceLines(statuses)...)
}

func (m Model) singleSourceLoadingLines(width int, source domain.SourceID, statuses []domain.SourceHealth, frame string) []string {
	status := loadingStatusForSource(statuses, source)
	label := app.SourceLabel(source)
	if strings.TrimSpace(frame) == "" {
		frame = "..."
	}
	return []string{
		m.styles.muted.Render("Loading " + label + "..."),
		clip(fmt.Sprintf("%s %s %s", frame, m.renderSourceBadge(source), m.styles.muted.Render(string(status))), width),
	}
}

func loadingStatusForSource(statuses []domain.SourceHealth, source domain.SourceID) domain.SourceStatus {
	for _, status := range statuses {
		if status.Source == source && status.Status != "" && status.Status != domain.SourceStatusUnknown {
			return status.Status
		}
	}
	return domain.SourceStatusRefreshing
}

func (m Model) loadingSourceLines(statuses []domain.SourceHealth) []string {
	statusBySource := make(map[domain.SourceID]domain.SourceStatus, len(statuses))
	for _, status := range statuses {
		statusBySource[status.Source] = status.Status
	}
	var lines []string
	for _, entry := range app.SourceCatalog() {
		source := entry.Source
		status := statusBySource[source]
		if status == "" || status == domain.SourceStatusUnknown {
			status = domain.SourceStatusRefreshing
		}
		lines = append(lines, fmt.Sprintf("   %s %s", m.renderSourceBadge(source), m.styles.muted.Render(string(status))))
	}
	return lines
}

func (m Model) loadingProgressBar(width int, statuses []domain.SourceHealth) string {
	if width < 12 {
		return ""
	}
	bar := progress.New(
		progress.WithColors(lipgloss.Color(m.styles.spec.active), lipgloss.Color(m.styles.spec.ok)),
		progress.WithScaled(true),
		progress.WithWidth(clamp(width, 12, 42)),
	)
	return clip(bar.ViewAs(loadingProgressPercent(statuses)), width)
}

func loadingProgressPercent(statuses []domain.SourceHealth) float64 {
	total := len(app.SourceCatalog())
	if total == 0 {
		return 1
	}
	statusBySource := make(map[domain.SourceID]domain.SourceStatus, len(statuses))
	for _, status := range statuses {
		statusBySource[status.Source] = status.Status
	}
	done := 0
	for _, entry := range app.SourceCatalog() {
		if sourceStatusComplete(statusBySource[entry.Source]) {
			done++
		}
	}
	if done == 0 {
		return 0.1
	}
	return min(0.95, float64(done)/float64(total))
}

func sourceStatusComplete(status domain.SourceStatus) bool {
	switch status {
	case domain.SourceStatusOK,
		domain.SourceStatusStale,
		domain.SourceStatusRateLimited,
		domain.SourceStatusAuthRequired,
		domain.SourceStatusParserBroken,
		domain.SourceStatusNetworkError,
		domain.SourceStatusDisabled:
		return true
	default:
		return false
	}
}
