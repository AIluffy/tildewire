package markdown

import (
	"strings"

	"github.com/AIluffy/tildewire/internal/config"
)

type markdownSegment struct {
	markdown string
	table    *markdownTableBlock
	image    *ImageRef
	code     *CodeBlock
	divider  *markdownDividerBlock
}

func renderMarkdownSegments(renderer markdownRenderer, segments []markdownSegment, width int, options Options) (RenderedDocument, error) {
	var rendered strings.Builder
	codeBlocks := make([]RenderedCodeBlock, 0)
	appendBlock := func(block string) int {
		if strings.TrimSpace(block) == "" {
			return strings.Count(rendered.String(), "\n")
		}
		if rendered.Len() > 0 {
			rendered.WriteString("\n\n")
		}
		startLine := strings.Count(rendered.String(), "\n")
		rendered.WriteString(block)
		return startLine
	}
	for _, segment := range segments {
		if segment.code != nil {
			block, meta := renderMarkdownCodeBlock(*segment.code, width, options)
			startLine := appendBlock(block)
			meta.StartLine += startLine
			meta.EndLine += startLine
			meta.CopyHit.Line += startLine
			codeBlocks = append(codeBlocks, meta)
			continue
		}
		if segment.table != nil {
			appendBlock(renderMarkdownTable(*segment.table, width, options.TableWrap, options.Theme))
			continue
		}
		if segment.image != nil {
			state, ok := imageStateForSegment(*segment.image, width, options)
			mode := options.ImageMode
			if !options.ImageSegments {
				mode = config.MarkdownImagePreviewOff
				state = ImageState{}
				ok = false
			}
			appendBlock(RenderImageSegment(*segment.image, width, mode, state, ok, options.Theme))
			continue
		}
		if segment.divider != nil {
			appendBlock(renderMarkdownDivider(width, options.Theme))
			continue
		}
		if strings.TrimSpace(segment.markdown) == "" {
			continue
		}
		block, err := renderer.Render(segment.markdown)
		if err != nil {
			return RenderedDocument{}, err
		}
		appendBlock(strings.TrimRight(block, "\n"))
	}
	return RenderedDocument{Content: rendered.String(), CodeBlocks: codeBlocks}, nil
}

func splitMarkdownSegments(markdown string, baseURL string) []markdownSegment {
	lines := strings.Split(markdown, "\n")
	segments := make([]markdownSegment, 0, 1)
	markdownLines := make([]string, 0, len(lines))

	flushMarkdown := func() {
		if len(markdownLines) == 0 {
			return
		}
		segments = append(segments, markdownSegment{markdown: strings.Join(markdownLines, "\n")})
		markdownLines = markdownLines[:0]
	}

	for i := 0; i < len(lines); {
		if block, next, ok := parseMarkdownCodeBlockAt(lines, i); ok {
			flushMarkdown()
			segments = append(segments, markdownSegment{code: &block})
			i = next
			continue
		}
		if isMarkdownDividerLine(lines[i]) && canSplitMarkdownDivider(markdownLines) {
			flushMarkdown()
			segments = append(segments, markdownSegment{divider: &markdownDividerBlock{}})
			i++
			continue
		}
		if images, next, ok := parseImageOnlyHTMLBlockAt(lines, i, baseURL); ok {
			flushMarkdown()
			appendImageSegments(&segments, images)
			i = next
			continue
		}
		if images := ParseImageLine(lines[i], baseURL); len(images) > 0 {
			flushMarkdown()
			appendImageSegments(&segments, images)
			i++
			continue
		}
		if block, next, ok := parseMarkdownTableAt(lines, i); ok {
			flushMarkdown()
			segments = append(segments, markdownSegment{table: &block})
			i = next
			continue
		}
		markdownLines = append(markdownLines, lines[i])
		i++
	}
	flushMarkdown()
	return segments
}

func appendImageSegments(segments *[]markdownSegment, images []ImageRef) {
	for _, image := range images {
		image := image
		*segments = append(*segments, markdownSegment{image: &image})
	}
}

func parseImageOnlyHTMLBlockAt(lines []string, start int, baseURL string) ([]ImageRef, int, bool) {
	if start >= len(lines) || strings.TrimSpace(lines[start]) == "" {
		return nil, start, false
	}
	blockLines := make([]string, 0)
	for i := start; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || !htmlImageBlockLine(trimmed) {
			break
		}
		blockLines = append(blockLines, trimmed)
	}
	if len(blockLines) == 0 {
		return nil, start, false
	}
	next := start + len(blockLines)
	if next < len(lines) && strings.TrimSpace(lines[next]) != "" {
		return nil, start, false
	}
	block := strings.Join(blockLines, " ")
	if !imageOnlyHTMLLine(block) {
		return nil, start, false
	}
	images := ParseImageLine(block, baseURL)
	if len(images) == 0 {
		return nil, start, false
	}
	return images, next, true
}

func htmlImageBlockLine(line string) bool {
	if !strings.Contains(line, "<") {
		return false
	}
	withoutImages := htmlImgTagPattern.ReplaceAllString(line, "")
	withoutTags := htmlTagPattern.ReplaceAllString(withoutImages, "")
	return strings.TrimSpace(withoutTags) == ""
}

func markdownSegmentsContainSpecial(segments []markdownSegment) bool {
	for _, segment := range segments {
		if segment.table != nil || segment.image != nil || segment.code != nil || segment.divider != nil {
			return true
		}
	}
	return false
}
