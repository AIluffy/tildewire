package image

import (
	"strings"
	"unicode/utf8"
)

// Placeholder returns a fixed-size text fallback for an image request.
func Placeholder(request ImageRequest) RenderedImage {
	rect := request.Rect
	width := maxInt(0, rect.Width)
	height := maxInt(0, rect.Height)
	if width == 0 || height == 0 {
		return RenderedImage{ID: request.ID, Protocol: ProtocolHalfblocks, Rect: rect}
	}
	alt := sanitizeAlt(request.AltText)
	if alt == "" {
		alt = "image"
	}
	lines := make([]string, 0, height)
	switch {
	case height == 1:
		lines = append(lines, padRight(clipRunes("["+alt+"]", width), width))
	case width < 4:
		for range height {
			lines = append(lines, strings.Repeat(" ", width))
		}
	default:
		border := "+" + strings.Repeat("-", width-2) + "+"
		lines = append(lines, border)
		contentWidth := width - 2
		for row := 1; row < height-1; row++ {
			text := ""
			if row == 1 {
				text = clipRunes(alt, contentWidth)
			}
			lines = append(lines, "|"+padRight(text, contentWidth)+"|")
		}
		if height > 1 {
			lines = append(lines, border)
		}
	}
	return RenderedImage{
		ID:          request.ID,
		Protocol:    ProtocolHalfblocks,
		Cells:       strings.Join(lines, "\n"),
		Rect:        rect,
		WidthCells:  width,
		HeightCells: height,
	}
}

func sanitizeAlt(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	return strings.TrimSpace(value)
}

func clipRunes(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if utf8.RuneCountInString(value) <= width {
		return value
	}
	var b strings.Builder
	count := 0
	for _, r := range value {
		if count >= width {
			break
		}
		b.WriteRune(r)
		count++
	}
	return b.String()
}

func padRight(value string, width int) string {
	count := utf8.RuneCountInString(value)
	if count >= width {
		return value
	}
	return value + strings.Repeat(" ", width-count)
}
