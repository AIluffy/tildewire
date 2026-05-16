package markdown

import (
	"image/color"
	"strings"

	"charm.land/glamour/v2"
	glamourStyles "charm.land/glamour/v2/styles"
)

// Options controls terminal Markdown rendering.
type Options struct {
	Style                string
	Width                int
	BaseURL              string
	TableWrap            bool
	CleanHeadingPrefixes bool
	ImageSegments        bool
	ImageMode            string
	Images               map[ImageKey]ImageState
	DarkBackground       bool
	TerminalBackground   color.Color
}

// RenderedDocument is terminal Markdown output plus metadata for interactive blocks.
type RenderedDocument struct {
	Content    string
	CodeBlocks []RenderedCodeBlock
}

// Render renders Markdown for terminal display.
func Render(markdown string, options Options) (string, error) {
	document, err := RenderDocument(markdown, options)
	if err != nil {
		return "", err
	}
	return document.Content, nil
}

// RenderDocument renders Markdown for terminal display and returns block metadata.
func RenderDocument(markdown string, options Options) (RenderedDocument, error) {
	style := strings.TrimSpace(options.Style)
	if style == "" {
		style = "dark"
	}
	width := max(1, options.Width)
	rendererOptions := []glamour.TermRendererOption{
		glamourStyleOption(style, options.CleanHeadingPrefixes),
		glamour.WithWordWrap(width),
		glamour.WithTableWrap(options.TableWrap),
	}
	if strings.TrimSpace(options.BaseURL) != "" {
		rendererOptions = append(rendererOptions, glamour.WithBaseURL(options.BaseURL))
	}
	renderer, err := glamour.NewTermRenderer(rendererOptions...)
	if err != nil {
		return RenderedDocument{}, err
	}
	segments := splitMarkdownSegments(markdown, options.BaseURL, options.ImageSegments)
	if markdownSegmentsContainSpecial(segments) {
		return renderMarkdownSegments(renderer, segments, width, options)
	}
	content, err := renderer.Render(markdown)
	if err != nil {
		return RenderedDocument{}, err
	}
	return RenderedDocument{Content: content}, nil
}

type markdownRenderer interface {
	Render(string) (string, error)
}

func glamourStyleOption(style string, cleanHeadingPrefixes bool) glamour.TermRendererOption {
	if !cleanHeadingPrefixes {
		return glamour.WithStandardStyle(style)
	}
	styleConfig, ok := glamourStyles.DefaultStyles[style]
	if !ok || styleConfig == nil {
		return glamour.WithStandardStyle(style)
	}
	config := *styleConfig
	config.H1.Prefix = cleanMarkdownHeadingPrefix(config.H1.Prefix)
	config.H2.Prefix = cleanMarkdownHeadingPrefix(config.H2.Prefix)
	config.H3.Prefix = cleanMarkdownHeadingPrefix(config.H3.Prefix)
	config.H4.Prefix = cleanMarkdownHeadingPrefix(config.H4.Prefix)
	config.H5.Prefix = cleanMarkdownHeadingPrefix(config.H5.Prefix)
	config.H6.Prefix = cleanMarkdownHeadingPrefix(config.H6.Prefix)
	return glamour.WithStyles(config)
}

func cleanMarkdownHeadingPrefix(prefix string) string {
	trimmed := strings.TrimSpace(prefix)
	if trimmed != "" && strings.Trim(trimmed, "#") == "" {
		return ""
	}
	return prefix
}
