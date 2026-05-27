package githubreadme

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

var (
	markdownLinkPattern = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	rawURLPattern       = regexp.MustCompile(`https?://[^\s<>\]\)]+`)
)

// Clean removes GitHub README chrome that is noisy in a terminal detail view.
func Clean(markdownText string) string {
	lines := strings.Split(markdownText, "\n")
	cleaned := make([]string, 0, len(lines))
	inCodeBlock := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inCodeBlock = !inCodeBlock
			cleaned = append(cleaned, line)
			continue
		}
		if !inCodeBlock && noiseLine(trimmed) {
			continue
		}
		if !inCodeBlock {
			line = cleanLine(line)
			cleaned = append(cleaned, expandInlineBlockquoteTableLine(line)...)
			continue
		}
		cleaned = append(cleaned, line)
	}
	return strings.TrimSpace(collapseBlankLines(cleaned))
}

func noiseLine(trimmed string) bool {
	if trimmed == "" {
		return false
	}
	if imageOnlyHTMLLine(trimmed) && htmlImageBadgeOnlyLine(trimmed) {
		return true
	}
	if htmlImageWrapperOnlyPattern.MatchString(trimmed) {
		return true
	}
	if htmlPictureSourcePattern.MatchString(trimmed) {
		return !imageOnlyHTMLLine(trimmed)
	}
	if markdownImagePattern.MatchString(trimmed) {
		return badgeOnlyLine(trimmed)
	}
	withoutImages := strings.TrimSpace(markdownImagePattern.ReplaceAllString(trimmed, ""))
	if withoutImages == "" {
		return false
	}
	withoutLinks := markdownLinkPattern.ReplaceAllString(withoutImages, "")
	withoutLinks = rawURLPattern.ReplaceAllString(withoutLinks, "")
	withoutLinks = strings.TrimSpace(withoutLinks)
	withoutLinks = strings.Trim(withoutLinks, "|-/\\,.:;_`*#[](){}<>")
	return withoutLinks == "" && withoutLinks != trimmed && (strings.Contains(trimmed, "](") || rawURLPattern.MatchString(trimmed))
}

func cleanLine(line string) string {
	line = stripBadgeImagesFromLine(line)
	trimmed := strings.TrimSpace(line)
	if imageOnlyHTMLLine(trimmed) {
		return renderHTMLImageMarkdown(trimmed)
	}
	if imageOnlyMarkdownLine(trimmed) {
		return strings.TrimRight(line, " \t")
	}
	line = renderImagePlaceholders(line)
	line = markdownLinkPattern.ReplaceAllString(line, "$1")
	line = rawURLPattern.ReplaceAllStringFunc(line, shortenURL)
	line = stripInlineHTML(line)
	return strings.TrimRight(line, " \t")
}

func badgeOnlyLine(line string) bool {
	matches := markdownImageWithPartsPattern.FindAllStringSubmatch(line, -1)
	if len(matches) == 0 {
		return false
	}
	for _, match := range matches {
		if len(match) != 3 || !isBadgeImageURL(strings.TrimSpace(match[2])) {
			return false
		}
	}
	withoutImages := markdownImagePattern.ReplaceAllString(line, "")
	withoutLinks := markdownLinkPattern.ReplaceAllString(withoutImages, "")
	withoutLinks = rawURLPattern.ReplaceAllString(withoutLinks, "")
	withoutLinks = strings.TrimSpace(withoutLinks)
	withoutLinks = strings.Trim(withoutLinks, "|-/\\,.:;_`*#[](){}<>")
	return withoutLinks == ""
}

func renderImagePlaceholders(value string) string {
	return markdownImagePattern.ReplaceAllStringFunc(value, func(match string) string {
		parts := markdownImageWithPartsPattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return "Image"
		}
		alt := strings.TrimSpace(parts[1])
		if alt == "" {
			alt = "image"
		}
		src := strings.TrimSpace(parts[2])
		if src == "" {
			return "Image: " + alt
		}
		if isBadgeImageURL(src) {
			return "Image: " + alt
		}
		if rawURLPattern.MatchString(src) {
			src = shortenURL(src)
		}
		return fmt.Sprintf("Image: %s (%s)", alt, src)
	})
}

func renderHTMLImageMarkdown(line string) string {
	refs := parseHTMLImageRefs(line)
	rendered := make([]string, 0, len(refs))
	for _, ref := range refs {
		if isBadgeImageURL(ref.Src) {
			continue
		}
		rendered = append(rendered, fmt.Sprintf("![%s](%s)", markdownImageAlt(ref.Alt), ref.Src))
	}
	return strings.Join(rendered, "\n")
}

func markdownImageAlt(alt string) string {
	alt = strings.TrimSpace(alt)
	if alt == "" {
		alt = "image"
	}
	alt = strings.ReplaceAll(alt, `\`, `\\`)
	alt = strings.ReplaceAll(alt, "]", `\]`)
	return alt
}

func stripInlineHTML(line string) string {
	line = htmlBreakTagPattern.ReplaceAllString(line, " ")
	line = htmlTagPattern.ReplaceAllString(line, "")
	return strings.TrimSpace(line)
}

func expandInlineBlockquoteTableLine(line string) []string {
	if !strings.Contains(line, "> |") {
		return []string{line}
	}
	parts := strings.Split(line, "> |")
	lines := make([]string, 0, len(parts))
	if prefix := strings.TrimSpace(parts[0]); prefix != "" {
		lines = append(lines, prefix)
	}
	for _, part := range parts[1:] {
		row := strings.TrimSpace(part)
		if row == "" {
			continue
		}
		row = "| " + strings.TrimPrefix(row, "|")
		lines = append(lines, strings.TrimSpace(row))
	}
	if len(lines) == 0 {
		return []string{line}
	}
	return lines
}

func shortenURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return clip(raw, 48)
	}
	display := parsed.Host + parsed.EscapedPath()
	if display == parsed.Host && parsed.RawQuery != "" {
		display += "?" + parsed.RawQuery
	}
	if display == "" {
		display = raw
	}
	return clip(display, 48)
}

func collapseBlankLines(lines []string) string {
	collapsed := make([]string, 0, len(lines))
	blank := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if blank {
				continue
			}
			blank = true
			collapsed = append(collapsed, "")
			continue
		}
		blank = false
		collapsed = append(collapsed, line)
	}
	return strings.Join(collapsed, "\n")
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
