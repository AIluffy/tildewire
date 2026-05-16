package markdown

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	chromaStyles "github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/x/ansi"
)

const (
	codeBlockCopyIcon     = "⧉"
	codeBlockInnerPadding = 2
)

// CodeBlock is a fenced Markdown code block.
type CodeBlock struct {
	language string
	lines    []string
}

// RenderedCodeBlock is a rendered fenced code block and its copy hit target.
type RenderedCodeBlock struct {
	Block     CodeBlock
	StartLine int
	EndLine   int
	CopyHit   CodeBlockCopyHit
}

// CodeBlockCopyHit is the rendered cell range used for mouse copy.
type CodeBlockCopyHit struct {
	Line   int
	StartX int
	EndX   int
}

// Text returns the code block body without Markdown fences.
func (b CodeBlock) Text() string {
	return strings.Join(b.lines, "\n")
}

// CodeBlocks extracts fenced Markdown code blocks.
func CodeBlocks(markdown string) []CodeBlock {
	lines := strings.Split(markdown, "\n")
	blocks := make([]CodeBlock, 0)
	for i := 0; i < len(lines); {
		block, next, ok := parseMarkdownCodeBlockAt(lines, i)
		if !ok {
			i++
			continue
		}
		blocks = append(blocks, block)
		i = next
	}
	return blocks
}

func renderMarkdownCodeBlock(block CodeBlock, width int, options Options) (string, RenderedCodeBlock) {
	const minCodeBlockWidth = 24
	width = max(minCodeBlockWidth, width)
	contentWidth := max(1, width-2)
	codeWidth := max(1, contentWidth-codeBlockInnerPadding*2)
	palette := codeBlockPaletteFor(options)
	headerLine, copyHitStart, copyHitEnd := renderCodeBlockHeader(block, contentWidth, palette)
	codeLines := highlightedCodeLines(block, codeWidth, palette)
	rendered := make([]string, 0, len(codeLines)+4)
	rendered = append(rendered, palette.borderStyle.Render("┌"+strings.Repeat("─", contentWidth)+"┐"))
	rendered = append(rendered, palette.borderStyle.Render("│")+headerLine+palette.borderStyle.Render("│"))
	rendered = append(rendered, palette.borderStyle.Render("├"+strings.Repeat("─", contentWidth)+"┤"))
	for _, line := range codeLines {
		rendered = append(rendered, palette.borderStyle.Render("│")+renderCodeLine(line, contentWidth, codeWidth, palette)+palette.borderStyle.Render("│"))
	}
	rendered = append(rendered, palette.borderStyle.Render("└"+strings.Repeat("─", contentWidth)+"┘"))
	meta := RenderedCodeBlock{
		Block:     block,
		StartLine: 0,
		EndLine:   len(rendered) - 1,
		CopyHit: CodeBlockCopyHit{
			Line:   1,
			StartX: copyHitStart,
			EndX:   copyHitEnd,
		},
	}
	return strings.Join(rendered, "\n"), meta
}

type codeBlockPalette struct {
	dark             bool
	background       color.Color
	headerBackground color.Color
	border           color.Color
	headerText       color.Color
	bodyText         color.Color
	borderStyle      lipgloss.Style
	headerStyle      lipgloss.Style
	bodyStyle        lipgloss.Style
	iconStyle        lipgloss.Style
}

func codeBlockPaletteFor(options Options) codeBlockPalette {
	base := terminalBackground(options)
	background := shiftColor(base, -0.06)
	headerBackground := shiftColor(base, -0.03)
	dividerText := "#52525B"
	bodyText := "#18181B"
	headerText := "#52525B"
	if options.DarkBackground {
		dividerText = "#A1A1AA"
		bodyText = "#E4E4E7"
		headerText = "#A1A1AA"
	}
	return codeBlockPalette{
		background:       background,
		headerBackground: headerBackground,
		border:           markdownTableBorderColor,
		headerText:       lipgloss.Color(headerText),
		bodyText:         lipgloss.Color(bodyText),
		dark:             options.DarkBackground,
		borderStyle:      lipgloss.NewStyle().Foreground(markdownTableBorderColor),
		headerStyle:      lipgloss.NewStyle().Background(headerBackground).Foreground(lipgloss.Color(headerText)),
		bodyStyle:        lipgloss.NewStyle().Background(background).Foreground(lipgloss.Color(bodyText)),
		iconStyle:        lipgloss.NewStyle().Background(headerBackground).Foreground(lipgloss.Color(dividerText)),
	}
}

func terminalBackground(options Options) color.Color {
	if options.TerminalBackground != nil {
		return options.TerminalBackground
	}
	return lipgloss.LightDark(options.DarkBackground)(lipgloss.Color("#F8FAFC"), lipgloss.Color("#20202C"))
}

func shiftColor(value color.Color, amount float64) color.Color {
	rgba := color.RGBAModel.Convert(value).(color.RGBA)
	shift := func(component uint8) uint8 {
		current := float64(component)
		if amount < 0 {
			return uint8(max(0, int(current*(1+amount))))
		}
		return uint8(min(255, int(current+(255-current)*amount)))
	}
	return color.RGBA{R: shift(rgba.R), G: shift(rgba.G), B: shift(rgba.B), A: 255}
}

func renderCodeBlockHeader(block CodeBlock, contentWidth int, palette codeBlockPalette) (string, int, int) {
	label := codeBlockLanguageLabel(block.language)
	iconWidth := lipgloss.Width(codeBlockCopyIcon)
	labelWidth := max(1, contentWidth-iconWidth-3)
	label = clip(label, labelWidth)
	left := " " + label
	gap := strings.Repeat(" ", max(1, contentWidth-lipgloss.Width(left)-iconWidth-1))
	right := palette.iconStyle.Render(codeBlockCopyIcon) + " "
	copyX := lipgloss.Width(left + gap)
	header := left + gap + right
	header += strings.Repeat(" ", max(0, contentWidth-ansi.StringWidth(header)))
	copyStart := max(1, copyX)
	copyEnd := 1 + contentWidth
	return palette.headerStyle.Render(header), copyStart, copyEnd
}

func renderCodeLine(line string, contentWidth int, codeWidth int, palette codeBlockPalette) string {
	clipped := clip(line, codeWidth)
	lineWidth := ansi.StringWidth(clipped)
	padding := strings.Repeat(" ", max(0, codeWidth-lineWidth))
	leftPad := palette.bodyStyle.Render(strings.Repeat(" ", codeBlockInnerPadding))
	rightPad := palette.bodyStyle.Render(padding + strings.Repeat(" ", codeBlockInnerPadding))
	rendered := leftPad + clipped + rightPad
	if width := ansi.StringWidth(rendered); width < contentWidth {
		rendered += palette.bodyStyle.Render(strings.Repeat(" ", contentWidth-width))
	}
	return rendered
}

func highlightedCodeLines(block CodeBlock, width int, palette codeBlockPalette) []string {
	lines := block.lines
	if len(lines) == 0 {
		lines = []string{""}
	}
	code := strings.Join(lines, "\n")
	lexer := lexers.Get(codeBlockLexerName(block.language))
	if lexer == nil {
		lexer = lexers.Fallback
	}
	iterator, err := chroma.Coalesce(lexer).Tokenise(nil, code)
	if err != nil {
		return plainCodeLines(lines, palette)
	}
	style := chromaStyles.Get(codeBlockChromaStyle(palette))
	rendered := renderHighlightedTokens(iterator, style, palette)
	if len(rendered) == 0 {
		return plainCodeLines(lines, palette)
	}
	for i, line := range rendered {
		rendered[i] = clip(line, width)
	}
	return rendered
}

func renderHighlightedTokens(iterator chroma.Iterator, style *chroma.Style, palette codeBlockPalette) []string {
	lines := []string{""}
	for token := iterator(); token != chroma.EOF; token = iterator() {
		parts := strings.Split(token.Value, "\n")
		for idx, part := range parts {
			if idx > 0 {
				lines = append(lines, "")
			}
			if part == "" {
				continue
			}
			lines[len(lines)-1] += renderToken(part, style.Get(token.Type), palette)
		}
	}
	return lines
}

func renderToken(value string, entry chroma.StyleEntry, palette codeBlockPalette) string {
	style := lipgloss.NewStyle().Background(palette.background)
	if entry.Colour.IsSet() {
		style = style.Foreground(lipgloss.Color(entry.Colour.String()))
	} else {
		style = style.Foreground(palette.bodyText)
	}
	if entry.Bold == chroma.Yes {
		style = style.Bold(true)
	}
	if entry.Italic == chroma.Yes {
		style = style.Italic(true)
	}
	if entry.Underline == chroma.Yes {
		style = style.Underline(true)
	}
	return style.Render(value)
}

func plainCodeLines(lines []string, palette codeBlockPalette) []string {
	rendered := make([]string, len(lines))
	for i, line := range lines {
		rendered[i] = palette.bodyStyle.Render(line)
	}
	return rendered
}

func codeBlockChromaStyle(palette codeBlockPalette) string {
	if palette.dark {
		return "github-dark"
	}
	return "github"
}

func codeBlockLanguageLabel(language string) string {
	label := codeBlockLexerName(language)
	if label == "" {
		return "text"
	}
	return label
}

func codeBlockLexerName(language string) string {
	language = strings.TrimSpace(language)
	if language == "" {
		return ""
	}
	fields := strings.Fields(language)
	if len(fields) == 0 {
		return ""
	}
	name := strings.Trim(fields[0], "{}")
	name = strings.TrimPrefix(name, ".")
	if name == "" {
		return ""
	}
	return strings.ToLower(name)
}

func isMarkdownFenceLine(trimmed string) bool {
	_, _, ok := markdownFence(trimmed)
	return ok
}

func parseMarkdownCodeBlockAt(lines []string, start int) (CodeBlock, int, bool) {
	opening := strings.TrimSpace(lines[start])
	marker, language, ok := markdownFence(opening)
	if !ok {
		return CodeBlock{}, start, false
	}
	codeLines := make([]string, 0)
	next := start + 1
	for next < len(lines) {
		trimmed := strings.TrimSpace(lines[next])
		if strings.HasPrefix(trimmed, marker) {
			return CodeBlock{language: language, lines: codeLines}, next + 1, true
		}
		codeLines = append(codeLines, lines[next])
		next++
	}
	return CodeBlock{language: language, lines: codeLines}, next, true
}

func markdownFence(trimmed string) (string, string, bool) {
	if len(trimmed) < 3 {
		return "", "", false
	}
	fenceChar := trimmed[0]
	if fenceChar != '`' && fenceChar != '~' {
		return "", "", false
	}
	end := 0
	for end < len(trimmed) && trimmed[end] == fenceChar {
		end++
	}
	if end < 3 {
		return "", "", false
	}
	return trimmed[:end], strings.TrimSpace(trimmed[end:]), true
}
