package markdown

import "strings"

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
			appendBlock(renderMarkdownTable(*segment.table, width, options.TableWrap))
			continue
		}
		if segment.image != nil {
			state, ok := imageStateForSegment(*segment.image, width, options)
			appendBlock(RenderImageSegment(*segment.image, width, options.ImageMode, state, ok))
			continue
		}
		if segment.divider != nil {
			appendBlock(renderMarkdownDivider(width))
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

func splitMarkdownSegments(markdown string, baseURL string, imageSegments bool) []markdownSegment {
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
		if imageSegments {
			if images := ParseImageLine(lines[i], baseURL); len(images) > 0 {
				flushMarkdown()
				for _, image := range images {
					image := image
					segments = append(segments, markdownSegment{image: &image})
				}
				i++
				continue
			}
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

func markdownSegmentsContainSpecial(segments []markdownSegment) bool {
	for _, segment := range segments {
		if segment.table != nil || segment.image != nil || segment.code != nil || segment.divider != nil {
			return true
		}
	}
	return false
}
