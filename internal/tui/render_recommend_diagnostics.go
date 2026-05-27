package tui

import (
	"fmt"
	"strings"

	"github.com/AIluffy/tildewire/internal/domain"
)

func (m Model) renderRecommendDiagnostics() string {
	width := clamp(m.width-4, 36, 90)
	lines := []string{m.styles.header.Render("RECOMMEND DIAGNOSTICS"), ""}
	if m.recommendDiagnosticsLoading {
		lines = append(lines, m.styles.muted.Render("Loading diagnostics..."), "")
	}
	if m.recommendDiagnosticsError != "" {
		lines = append(lines, m.styles.err.Render(clip("Error: "+m.recommendDiagnosticsError, width-2)), "")
	}
	lines = append(lines, m.recommendDiagnosticsProfileLines(width)...)
	lines = append(lines, m.recommendDiagnosticsSelectedLines(width)...)
	lines = append(lines, "", m.styles.muted.Render("esc back  ? help  q quit"))
	if m.help.ShowAll {
		lines = append(lines, "", m.help.View(m.keys))
	}
	return m.styles.panel.Width(width).Render(strings.Join(lines, "\n"))
}

func (m Model) recommendDiagnosticsProfileLines(width int) []string {
	diagnostics := m.recommendDiagnostics
	lines := []string{m.styles.header.Render("Profile")}
	if len(diagnostics.PositiveTerms) == 0 && len(diagnostics.NegativeTerms) == 0 {
		return append(lines, m.styles.muted.Render("No profile terms yet."), "")
	}
	if len(diagnostics.PositiveTerms) > 0 {
		lines = append(lines, m.styles.ok.Render("Positive"))
		for _, term := range diagnostics.PositiveTerms {
			lines = append(lines, clip(fmt.Sprintf("  %s:%s +%.2f", term.Kind, term.Value, term.Positive), width-2))
		}
	}
	if len(diagnostics.NegativeTerms) > 0 {
		lines = append(lines, m.styles.warn.Render("Negative"))
		for _, term := range diagnostics.NegativeTerms {
			lines = append(lines, clip(fmt.Sprintf("  %s:%s -%.2f", term.Kind, term.Value, term.Negative), width-2))
		}
	}
	return append(lines, "")
}

func (m Model) recommendDiagnosticsSelectedLines(width int) []string {
	selected := m.recommendDiagnostics.Selected
	lines := []string{m.styles.header.Render("Selected Item")}
	if selected.ItemID == "" {
		return append(lines, m.styles.muted.Render("No selected item."))
	}
	title := selected.Title
	if title == "" {
		title = selected.ItemID
	}
	lines = append(lines, clip(title, width-2))
	status := "eligible"
	if !selected.Eligible {
		status = "excluded: " + emptyAsUnknown(selected.ExclusionReason)
	}
	lines = append(lines,
		clip("status "+status, width-2),
		clip(fmt.Sprintf("interest %.2f  hot %.2f  score %.2f", selected.InterestScore, selected.HotScore, selected.Score), width-2),
		clip("positive gate "+boolLabel(selected.HasPositiveSignal), width-2),
	)
	lines = append(lines, "", m.styles.header.Render("Matched Terms"))
	if len(selected.MatchedTerms) == 0 {
		lines = append(lines, m.styles.muted.Render("No matched profile terms."))
	} else {
		for _, term := range selected.MatchedTerms {
			lines = append(lines, clip(formatDiagnosticsTerm(term), width-2))
		}
	}
	lines = append(lines, "", m.styles.header.Render("Reasons"))
	if len(selected.Reasons) == 0 {
		lines = append(lines, m.styles.muted.Render("No recommendation reasons."))
		return lines
	}
	for _, reason := range selected.Reasons {
		lines = append(lines, clip(fmt.Sprintf("  %s %.2f", reason.Label, reason.Weight), width-2))
	}
	return lines
}

func formatDiagnosticsTerm(term domain.RecommendationDiagnosticsTerm) string {
	return fmt.Sprintf("  %s:%s %+0.2f  item %.2f  +%.2f -%.2f",
		term.Kind,
		term.Value,
		term.Contribution,
		term.ItemWeight,
		term.Positive,
		term.Negative,
	)
}

func emptyAsUnknown(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return value
}
