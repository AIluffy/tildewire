package tui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

func fillLines(lines []string, height int) string {
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

func renderedBlockHeight(block string) int {
	if block == "" {
		return 0
	}
	return strings.Count(block, "\n") + 1
}

func clipBlock(value string, width int) string {
	lines := strings.Split(value, "\n")
	for idx, line := range lines {
		if _, ok := detailRawLine(line); ok {
			continue
		}
		lines[idx] = clip(line, width)
	}
	return strings.Join(lines, "\n")
}

func placeBlock(block string, x, y, width int) string {
	lines := make([]string, 0, y+strings.Count(block, "\n")+1)
	for range y {
		lines = append(lines, "")
	}
	prefix := strings.Repeat(" ", max(0, x))
	for _, line := range strings.Split(block, "\n") {
		lines = append(lines, clip(prefix+line, width))
	}
	return strings.Join(lines, "\n")
}

func wrapFirst(value string, width int) string {
	lines := wrap(value, width)
	if len(lines) == 0 {
		return ""
	}
	return lines[0]
}

func wrap(value string, width int) []string {
	if width <= 8 {
		return []string{clip(value, width)}
	}
	words := strings.Fields(value)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	line := words[0]
	if ansi.StringWidth(line) > width {
		lines = append(lines, clip(line, width))
		line = ""
	}
	for _, word := range words[1:] {
		if line == "" {
			if ansi.StringWidth(word) > width {
				lines = append(lines, clip(word, width))
			} else {
				line = word
			}
			continue
		}
		if ansi.StringWidth(line)+1+ansi.StringWidth(word) > width {
			lines = append(lines, clip(line, width))
			if ansi.StringWidth(word) > width {
				lines = append(lines, clip(word, width))
				line = ""
				continue
			}
			line = word
			continue
		}
		line += " " + word
	}
	if line != "" {
		lines = append(lines, clip(line, width))
	}
	return lines
}

func clip(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(value) <= width {
		return value
	}
	if width <= 1 {
		return ansi.Truncate(value, width, "")
	}
	return ansi.Truncate(value, width, ".")
}

func clamp(value, low, high int) int {
	return min(max(value, low), high)
}

func feedVisibleRows(height int) int {
	return feedVisibleRowsFor(height, 1)
}

func feedVisibleRowsFor(height, topRows int) int {
	return max(1, (max(1, height)-max(1, topRows))/feedItemBlockRows)
}

func maxFeedOffset(totalRows, visibleRows int) int {
	if visibleRows <= 0 {
		return 0
	}
	return max(0, totalRows-visibleRows)
}

func maxSourceOffset(totalRows, visibleRows int) int {
	if visibleRows <= 0 {
		return 0
	}
	return max(0, totalRows-visibleRows)
}
