package markdown

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/zhangxueai/tildewire/internal/config"
)

const (
	// RawLinePrefix marks raw terminal image placeholder lines.
	RawLinePrefix           = "\x1dtildewire-raw-detail:"
	imagePreviewVerticalGap = 1
	rawLineSeparator        = "\x1e"
)

var (
	markdownImagePattern          = regexp.MustCompile(`!\[[^\]]*\]\([^)]+\)`)
	markdownImageWithPartsPattern = regexp.MustCompile(`!\[([^\]]*)\]\(([^)]+)\)`)
	markdownLinkedImagePattern    = regexp.MustCompile(`\[!\[([^\]]*)\]\(([^)]+)\)\]\([^)]+\)`)
	htmlImgTagPattern             = regexp.MustCompile(`(?i)<img\b[^>]*>`)
	htmlAttrPattern               = regexp.MustCompile(`(?i)\b([a-z0-9_-]+)\s*=\s*("([^"]*)"|'([^']*)'|([^\s>]+))`)
	htmlTagPattern                = regexp.MustCompile(`(?i)<\s*/?\s*[a-z][a-z0-9:-]*\b[^>]*>`)
)

// ImageRef is a Markdown or HTML image reference resolved for display.
type ImageRef struct {
	Alt string
	Src string
	URL string
}

// ImageKey identifies a cached Markdown image preview.
type ImageKey struct {
	URL   string
	Mode  string
	Width int
}

// ImageState stores Markdown image preview state held by the TUI model.
type ImageState struct {
	Alt     string
	Src     string
	URL     string
	Loading bool
	Content string
	Backend string
	Raw     bool
	Columns int
	Rows    int
	Err     string
}

// ParseImageLine parses an image-only Markdown or HTML line.
func ParseImageLine(line string, baseURL string) []ImageRef {
	rawRefs := parseImageOnlyLine(strings.TrimSpace(line))
	refs := make([]ImageRef, 0, len(rawRefs))
	seen := make(map[string]bool)
	for _, ref := range rawRefs {
		resolved, ok := resolveImageURL(ref.Src, baseURL)
		if !ok || seen[resolved] {
			continue
		}
		ref.URL = resolved
		refs = append(refs, ref)
		seen[resolved] = true
	}
	return refs
}

// RenderImageSegment renders one image segment using the provided preview state.
func RenderImageSegment(ref ImageRef, width int, mode string, state ImageState, ok bool) string {
	label := fmt.Sprintf("Image: %s", imageAltOrDefault(ref.Alt))
	displayURL := ref.Src
	if ok && state.Src != "" {
		displayURL = state.Src
	}
	if displayURL == "" {
		displayURL = ref.URL
	}
	mode = ScrollableImagePreviewMode(mode)
	if mode == config.MarkdownImagePreviewOff {
		return fmt.Sprintf("%s (%s)", label, displayURL)
	}
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#71717A"))
	if !ok {
		return mutedStyle.Render(fmt.Sprintf("%s (%s)", label, displayURL))
	}
	if state.Loading {
		return mutedStyle.Render(fmt.Sprintf("%s (%s) loading...", label, displayURL))
	}
	if state.Err != "" {
		return mutedStyle.Render(fmt.Sprintf("%s (%s) preview unavailable", label, displayURL))
	}
	lines := appendImagePreviewTopGap(nil)
	if state.Raw {
		rawContent := strings.TrimRight(state.Content, "\n")
		rawColumns := state.Columns
		if rawColumns <= 0 {
			rawColumns = width
		}
		rawRows := max(1, state.Rows)
		if rawContent != "" {
			lines = append(lines, MakeRawLine(rawContent, rawColumns))
		}
		for range max(0, rawRows-1) {
			lines = append(lines, "")
		}
		lines = appendImagePreviewBottomGap(lines)
		return strings.Join(lines, "\n")
	}
	contentLines := centeredImagePreviewLines(strings.Split(strings.TrimRight(state.Content, "\n"), "\n"), width)
	for _, line := range contentLines {
		lines = append(lines, line)
	}
	lines = appendImagePreviewBottomGap(lines)
	return strings.Join(lines, "\n")
}

// MakeRawLine encodes raw terminal image content as a placeholder line.
func MakeRawLine(line string, columns int) string {
	columns = max(1, columns)
	return RawLinePrefix + strconv.Itoa(columns) + rawLineSeparator + line
}

// RawLine extracts raw terminal image content from an encoded placeholder line.
func RawLine(line string) (string, bool) {
	raw, _, ok := RawLineInfo(line)
	return raw, ok
}

// RawLineInfo extracts raw terminal image content and its display columns.
func RawLineInfo(line string) (string, int, bool) {
	raw, ok := strings.CutPrefix(line, RawLinePrefix)
	if !ok {
		return "", 0, false
	}
	columnsValue, rawContent, found := strings.Cut(raw, rawLineSeparator)
	if !found {
		return raw, 1, true
	}
	columns, err := strconv.Atoi(columnsValue)
	if err != nil || columns <= 0 {
		columns = 1
	}
	return rawContent, columns, true
}

func imageStateForSegment(ref ImageRef, width int, options Options) (ImageState, bool) {
	mode := ScrollableImagePreviewMode(options.ImageMode)
	if state, ok := options.Images[ImageKey{URL: ref.URL, Mode: mode, Width: min(width, 96)}]; ok {
		return state, true
	}
	for _, state := range options.Images {
		if state.Src != "" && state.Src == ref.Src {
			return state, true
		}
	}
	return ImageState{}, false
}

func parseImageOnlyLine(line string) []ImageRef {
	switch {
	case imageOnlyMarkdownLine(line):
		return parseMarkdownImageRefs(line)
	case imageOnlyHTMLLine(line):
		return parseHTMLImageRefs(line)
	default:
		return nil
	}
}

func imageOnlyMarkdownLine(line string) bool {
	if !markdownImagePattern.MatchString(line) {
		return false
	}
	withoutLinkedImages := markdownLinkedImagePattern.ReplaceAllString(line, "")
	withoutImages := markdownImagePattern.ReplaceAllString(withoutLinkedImages, "")
	return strings.TrimSpace(withoutImages) == ""
}

func parseMarkdownImageRefs(line string) []ImageRef {
	matches := markdownImageWithPartsPattern.FindAllStringSubmatch(line, -1)
	refs := make([]ImageRef, 0, len(matches))
	for _, match := range matches {
		if len(match) != 3 {
			continue
		}
		src := cleanMarkdownImageDestination(match[2])
		if src == "" {
			continue
		}
		refs = append(refs, ImageRef{
			Alt: imageAltOrDefault(match[1]),
			Src: src,
		})
	}
	return refs
}

func imageOnlyHTMLLine(line string) bool {
	if !htmlImgTagPattern.MatchString(line) {
		return false
	}
	withoutImages := htmlImgTagPattern.ReplaceAllString(line, "")
	withoutTags := htmlTagPattern.ReplaceAllString(withoutImages, "")
	return strings.TrimSpace(withoutTags) == ""
}

func parseHTMLImageRefs(line string) []ImageRef {
	tags := htmlImgTagPattern.FindAllString(line, -1)
	refs := make([]ImageRef, 0, len(tags))
	for _, tag := range tags {
		attrs := htmlAttrs(tag)
		src := cleanMarkdownImageDestination(attrs["src"])
		if src == "" {
			src = firstHTMLSrcSetCandidate(attrs["srcset"])
		}
		if src == "" {
			continue
		}
		refs = append(refs, ImageRef{
			Alt: imageAltOrDefault(attrs["alt"]),
			Src: src,
		})
	}
	return refs
}

func firstHTMLSrcSetCandidate(srcset string) string {
	for _, candidate := range strings.Split(srcset, ",") {
		fields := strings.Fields(strings.TrimSpace(candidate))
		if len(fields) > 0 {
			return cleanMarkdownImageDestination(fields[0])
		}
	}
	return ""
}

func imageAltOrDefault(alt string) string {
	alt = strings.TrimSpace(alt)
	if alt == "" {
		return "image"
	}
	return alt
}

func htmlAttrs(tag string) map[string]string {
	attrs := make(map[string]string)
	for _, match := range htmlAttrPattern.FindAllStringSubmatch(tag, -1) {
		if len(match) != 6 {
			continue
		}
		value := match[3]
		if value == "" {
			value = match[4]
		}
		if value == "" {
			value = match[5]
		}
		attrs[strings.ToLower(match[1])] = value
	}
	return attrs
}

func cleanMarkdownImageDestination(destination string) string {
	destination = strings.TrimSpace(destination)
	destination = strings.Trim(destination, "<>")
	if strings.ContainsAny(destination, " \t") {
		fields := strings.Fields(destination)
		if len(fields) > 0 {
			destination = fields[0]
		}
	}
	return strings.TrimSpace(destination)
}

func resolveImageURL(src, baseURL string) (string, bool) {
	src = cleanMarkdownImageDestination(src)
	if src == "" || strings.HasPrefix(src, "#") {
		return "", false
	}
	parsedSrc, err := url.Parse(src)
	if err == nil && parsedSrc.IsAbs() {
		if parsedSrc.Scheme == "http" || parsedSrc.Scheme == "https" {
			return parsedSrc.String(), true
		}
		return "", false
	}
	if strings.HasPrefix(src, "//") {
		return "https:" + src, true
	}
	if strings.TrimSpace(baseURL) == "" {
		return src, true
	}
	parsedBase, err := url.Parse(baseURL)
	if err != nil {
		return src, true
	}
	parsedRef, err := url.Parse(src)
	if err != nil {
		return src, true
	}
	resolved := parsedBase.ResolveReference(parsedRef)
	return resolved.String(), true
}

func renderMarkdownImagePlaceholders(value string) string {
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
		return fmt.Sprintf("Image: %s (%s)", alt, src)
	})
}

func appendImagePreviewTopGap(lines []string) []string {
	for range imagePreviewVerticalGap {
		lines = append(lines, "")
	}
	return lines
}

func appendImagePreviewBottomGap(lines []string) []string {
	for range imagePreviewVerticalGap {
		lines = append(lines, "")
	}
	return lines
}

func centeredImagePreviewLines(lines []string, width int) []string {
	centered := make([]string, 0, len(lines))
	for _, line := range lines {
		lineWidth := ansi.StringWidth(line)
		if lineWidth <= 0 || lineWidth >= width {
			centered = append(centered, line)
			continue
		}
		centered = append(centered, strings.Repeat(" ", (width-lineWidth)/2)+line)
	}
	return centered
}
