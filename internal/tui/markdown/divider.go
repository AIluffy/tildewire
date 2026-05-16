package markdown

import (
	"strings"

	"charm.land/lipgloss/v2"
)

type markdownDividerBlock struct{}

const markdownDividerMargin = 2

func renderMarkdownDivider(width int) string {
	width = max(1, width)
	margin := min(markdownDividerMargin, max(0, (width-8)/2))
	dividerWidth := max(8, width-margin*2)
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#71717A")).Render(strings.Repeat(" ", margin) + strings.Repeat("─", dividerWidth))
}

func canSplitMarkdownDivider(markdownLines []string) bool {
	if len(markdownLines) == 0 {
		return true
	}
	previous := strings.TrimSpace(markdownLines[len(markdownLines)-1])
	return previous == "" || strings.HasPrefix(previous, ">")
}

func isMarkdownDividerLine(line string) bool {
	if countLeadingSpaces(line) > 3 {
		return false
	}
	markerKind := rune(0)
	markers := 0
	for _, r := range strings.TrimSpace(line) {
		if r == ' ' || r == '\t' {
			continue
		}
		kind, ok := markdownDividerMarkerKind(r)
		if !ok {
			return false
		}
		if markerKind == 0 {
			markerKind = kind
		}
		if markerKind != kind {
			return false
		}
		markers++
	}
	return markers >= 3
}

func markdownDividerMarkerKind(r rune) (rune, bool) {
	switch r {
	case '-', '—':
		return '-', true
	case '*':
		return '*', true
	case '_':
		return '_', true
	default:
		return 0, false
	}
}

func countLeadingSpaces(value string) int {
	spaces := 0
	for _, r := range value {
		if r != ' ' {
			return spaces
		}
		spaces++
	}
	return spaces
}
