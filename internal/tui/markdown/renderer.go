package markdown

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/glamour/v2"
	glamourANSI "charm.land/glamour/v2/ansi"
	glamourStyles "charm.land/glamour/v2/styles"
)

// Theme carries terminal theme colors into Markdown rendering.
type Theme struct {
	Accent   string
	Muted    string
	Text     string
	CodeText string
}

// Options controls terminal Markdown rendering.
type Options struct {
	Style                string
	Theme                Theme
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
		glamourStyleOption(style, options.CleanHeadingPrefixes, options.Theme, options),
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
	segments := splitMarkdownSegments(markdown, options.BaseURL)
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

func glamourStyleOption(style string, cleanHeadingPrefixes bool, theme Theme, options Options) glamour.TermRendererOption {
	if !cleanHeadingPrefixes && theme.empty() && options.TerminalBackground == nil {
		return glamour.WithStandardStyle(style)
	}
	styleConfig, ok := glamourStyles.DefaultStyles[style]
	if !ok || styleConfig == nil {
		return glamour.WithStandardStyle(style)
	}
	config := *styleConfig
	if cleanHeadingPrefixes {
		config.H1.Prefix = cleanMarkdownHeadingPrefix(config.H1.Prefix)
		config.H2.Prefix = cleanMarkdownHeadingPrefix(config.H2.Prefix)
		config.H3.Prefix = cleanMarkdownHeadingPrefix(config.H3.Prefix)
		config.H4.Prefix = cleanMarkdownHeadingPrefix(config.H4.Prefix)
		config.H5.Prefix = cleanMarkdownHeadingPrefix(config.H5.Prefix)
		config.H6.Prefix = cleanMarkdownHeadingPrefix(config.H6.Prefix)
	}
	applyGlamourTheme(&config, theme)
	applyAdaptiveInlineCodeStyle(&config, options)
	return glamour.WithStyles(config)
}

func applyGlamourTheme(config *glamourANSI.StyleConfig, theme Theme) {
	if theme.empty() {
		return
	}
	if text := strings.TrimSpace(theme.Text); text != "" {
		config.Document.Color = &text
		config.List.Color = &text
	}
	if accent := strings.TrimSpace(theme.Accent); accent != "" {
		config.Heading.Color = &accent
		config.Link.Color = &accent
		config.LinkText.Color = &accent
		config.Image.Color = &accent
		config.ImageText.Color = &accent
		config.HorizontalRule.Color = &accent
	}
	if muted := strings.TrimSpace(theme.Muted); muted != "" {
		config.BlockQuote.Color = &muted
	}
	if code := strings.TrimSpace(theme.CodeText); code != "" {
		config.Code.Color = &code
		config.CodeBlock.Color = &code
	}
}

func applyAdaptiveInlineCodeStyle(config *glamourANSI.StyleConfig, options Options) {
	if options.TerminalBackground == nil {
		return
	}
	palette := codeBlockPaletteFor(options)
	foreground := colorToHex(palette.bodyText)
	background := colorToHex(palette.background)
	config.Code.Color = &foreground
	config.Code.BackgroundColor = &background
}

func (theme Theme) empty() bool {
	return strings.TrimSpace(theme.Accent) == "" &&
		strings.TrimSpace(theme.Muted) == "" &&
		strings.TrimSpace(theme.Text) == "" &&
		strings.TrimSpace(theme.CodeText) == ""
}

func cleanMarkdownHeadingPrefix(prefix string) string {
	trimmed := strings.TrimSpace(prefix)
	if trimmed != "" && strings.Trim(trimmed, "#") == "" {
		return ""
	}
	return prefix
}

func colorToHex(value color.Color) string {
	rgba := color.RGBAModel.Convert(value).(color.RGBA)
	return fmt.Sprintf("#%02X%02X%02X", rgba.R, rgba.G, rgba.B)
}
