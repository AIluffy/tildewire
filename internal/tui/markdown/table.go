package markdown

import (
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
)

var (
	markdownLinkPattern      = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	markdownCodePattern      = regexp.MustCompile("`([^`]+)`")
	markdownStrongPattern    = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	tableDelimiterPattern    = regexp.MustCompile(`^:?-{3,}:?$`)
	markdownTableBorderColor = lipgloss.Color("#7D56F4")
)

type markdownTableBlock struct {
	headers    []string
	alignments []lipgloss.Position
	rows       [][]string
}

func renderMarkdownTable(block markdownTableBlock, width int, wrap bool) string {
	gray := lipgloss.Color("#A1A1AA")
	lightGray := lipgloss.Color("#D4D4D8")
	borderStyle := lipgloss.NewStyle().Foreground(markdownTableBorderColor)
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(markdownTableBorderColor).Padding(0, 1)
	evenRowStyle := lipgloss.NewStyle().Foreground(lightGray).Padding(0, 1)
	oddRowStyle := lipgloss.NewStyle().Foreground(gray).Padding(0, 1)

	t := table.New().
		Width(width).
		Wrap(wrap).
		Border(lipgloss.NormalBorder()).
		BorderStyle(borderStyle).
		StyleFunc(func(row, col int) lipgloss.Style {
			var style lipgloss.Style
			switch {
			case row == table.HeaderRow:
				style = headerStyle
			case row%2 == 0:
				style = evenRowStyle
			default:
				style = oddRowStyle
			}
			if col >= 0 && col < len(block.alignments) {
				style = style.Align(block.alignments[col])
			}
			return style
		}).
		Headers(block.headers...).
		Rows(block.rows...)

	return t.String()
}

func parseMarkdownTableAt(lines []string, start int) (markdownTableBlock, int, bool) {
	if start+1 >= len(lines) {
		return markdownTableBlock{}, start, false
	}
	headers, ok := parseMarkdownTableRow(lines[start])
	if !ok || len(headers) < 2 {
		return markdownTableBlock{}, start, false
	}
	alignments, ok := parseMarkdownTableDelimiter(lines[start+1], len(headers))
	if !ok {
		return markdownTableBlock{}, start, false
	}

	rows := make([][]string, 0)
	next := start + 2
	for next < len(lines) {
		if strings.TrimSpace(lines[next]) == "" {
			break
		}
		row, ok := parseMarkdownTableRow(lines[next])
		if !ok {
			break
		}
		rows = append(rows, normalizeMarkdownTableRow(row, len(headers)))
		next++
	}

	return markdownTableBlock{
		headers:    normalizeMarkdownTableRow(headers, len(headers)),
		alignments: alignments,
		rows:       rows,
	}, next, true
}

func parseMarkdownTableDelimiter(line string, columns int) ([]lipgloss.Position, bool) {
	cells, ok := splitMarkdownTableCells(line)
	if !ok || len(cells) != columns {
		return nil, false
	}
	alignments := make([]lipgloss.Position, len(cells))
	for i, cell := range cells {
		trimmed := strings.ReplaceAll(strings.TrimSpace(cell), " ", "")
		if !tableDelimiterPattern.MatchString(trimmed) {
			return nil, false
		}
		left := strings.HasPrefix(trimmed, ":")
		right := strings.HasSuffix(trimmed, ":")
		switch {
		case left && right:
			alignments[i] = lipgloss.Center
		case right:
			alignments[i] = lipgloss.Right
		default:
			alignments[i] = lipgloss.Left
		}
	}
	return alignments, true
}

func parseMarkdownTableRow(line string) ([]string, bool) {
	cells, ok := splitMarkdownTableCells(line)
	if !ok {
		return nil, false
	}
	for i, cell := range cells {
		cells[i] = renderMarkdownTableCell(cell)
	}
	return cells, true
}

func splitMarkdownTableCells(line string) ([]string, bool) {
	trimmed := strings.TrimSpace(line)
	if !hasUnescapedPipe(trimmed) {
		return nil, false
	}
	cells := splitUnescapedPipes(trimmed)
	if len(cells) > 0 && strings.TrimSpace(cells[0]) == "" && strings.HasPrefix(trimmed, "|") {
		cells = cells[1:]
	}
	if len(cells) > 0 && strings.TrimSpace(cells[len(cells)-1]) == "" && hasTrailingUnescapedPipe(trimmed) {
		cells = cells[:len(cells)-1]
	}
	if len(cells) < 2 {
		return nil, false
	}
	for i, cell := range cells {
		cells[i] = strings.TrimSpace(cell)
	}
	return cells, true
}

func splitUnescapedPipes(value string) []string {
	cells := make([]string, 0, strings.Count(value, "|")+1)
	var b strings.Builder
	escaped := false
	for _, r := range value {
		if escaped {
			if r != '|' {
				b.WriteRune('\\')
			}
			b.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '|' {
			cells = append(cells, b.String())
			b.Reset()
			continue
		}
		b.WriteRune(r)
	}
	if escaped {
		b.WriteRune('\\')
	}
	cells = append(cells, b.String())
	return cells
}

func hasUnescapedPipe(value string) bool {
	escaped := false
	for _, r := range value {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '|' {
			return true
		}
	}
	return false
}

func hasTrailingUnescapedPipe(value string) bool {
	if !strings.HasSuffix(value, "|") {
		return false
	}
	backslashes := 0
	for i := len(value) - 2; i >= 0 && value[i] == '\\'; i-- {
		backslashes++
	}
	return backslashes%2 == 0
}

func normalizeMarkdownTableRow(cells []string, columns int) []string {
	row := make([]string, columns)
	copy(row, cells)
	return row
}

func renderMarkdownTableCell(cell string) string {
	value := strings.TrimSpace(cell)
	value = renderMarkdownImagePlaceholders(value)
	value = markdownLinkPattern.ReplaceAllString(value, "$1")
	value = markdownCodePattern.ReplaceAllString(value, "$1")
	value = renderMarkdownStrongSpans(value)
	return strings.TrimSpace(value)
}

func renderMarkdownStrongSpans(value string) string {
	return markdownStrongPattern.ReplaceAllStringFunc(value, func(match string) string {
		parts := markdownStrongPattern.FindStringSubmatch(match)
		if len(parts) != 2 {
			return match
		}
		return lipgloss.NewStyle().Bold(true).Render(parts[1])
	})
}
