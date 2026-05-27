package markdown

import (
	"image/color"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/AIluffy/tildewire/internal/config"
	tuiimage "github.com/AIluffy/tildewire/internal/tui/image"
)

func TestRenderUsesLipglossTableStyle(t *testing.T) {
	markdown := strings.Join([]string{
		"| What | How | Speed |",
		"| --- | --- | ---: |",
		"| Pose estimation | CSI subcarrier amplitude/phase | 171K emb/s |",
		"| Breathing detection | Bandpass filtering | 6-30 BPM |",
	}, "\n")

	rendered, err := Render(markdown, Options{Style: "dark", Width: 88, TableWrap: true})
	if err != nil {
		t.Fatalf("render markdown: %v", err)
	}
	visible := ansi.Strip(rendered)
	for _, want := range []string{"┌", "┐", "└", "┘", "What", "How", "Speed", "Pose estimation"} {
		if !strings.Contains(visible, want) {
			t.Fatalf("styled table missing %q:\n%s", want, visible)
		}
	}
	if strings.Contains(visible, "| --- |") {
		t.Fatalf("styled table should not expose raw markdown delimiter:\n%s", visible)
	}
}

func TestRenderCodeBlockUsesPaddingAndCopyAffordance(t *testing.T) {
	markdown := strings.Join([]string{
		"```sh",
		"# Download DMG, EXEs over at https://tinyhumans.ai/openhuman",
		"",
		"curl -fsSL https://raw.githubusercontent.com/tinyhumansai/openhuman/main/scripts/install.sh | bash",
		"```",
	}, "\n")

	rendered, err := Render(markdown, Options{Style: "dark", Width: 96, TableWrap: true})
	if err != nil {
		t.Fatalf("render markdown: %v", err)
	}
	visible := ansi.Strip(rendered)
	for _, want := range []string{"sh", "  # Download DMG", "  curl -fsSL", "⧉"} {
		if !strings.Contains(visible, want) {
			t.Fatalf("padded code block missing %q:\n%s", want, visible)
		}
	}
	if strings.Contains(visible, "c copy") {
		t.Fatalf("code block should render copy icon instead of old text affordance:\n%s", visible)
	}
	if strings.Contains(visible, "```") {
		t.Fatalf("raw markdown fence leaked:\n%s", visible)
	}
	if !strings.Contains(rendered, "48;") {
		t.Fatalf("code block should use a distinct background:\n%q", rendered)
	}
}

func TestRenderCodeBlockHighlightsKnownLanguages(t *testing.T) {
	for _, tc := range []struct {
		name     string
		language string
		code     string
	}{
		{name: "go", language: "go", code: `fmt.Println("tildewire")`},
		{name: "shell", language: "sh", code: `curl -fsSL https://example.com/install.sh | bash`},
		{name: "unknown", language: "tildewirelang", code: `plain text fallback`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			markdown := strings.Join([]string{
				"```" + tc.language,
				tc.code,
				"```",
			}, "\n")

			rendered, err := Render(markdown, Options{Style: "dark", Width: 88, TableWrap: true, DarkBackground: true})
			if err != nil {
				t.Fatalf("render markdown: %v", err)
			}
			visible := ansi.Strip(rendered)
			for _, want := range []string{tc.language, tc.code, "⧉"} {
				if !strings.Contains(visible, want) {
					t.Fatalf("rendered code block missing %q:\n%s", want, visible)
				}
			}
			if strings.Contains(visible, "```") {
				t.Fatalf("raw markdown fence leaked:\n%s", visible)
			}
			if tc.language != "tildewirelang" && !strings.Contains(rendered, "\x1b[") {
				t.Fatalf("known language should render ANSI-highlighted code:\n%q", rendered)
			}
		})
	}
}

func TestRenderCodeBlockUsesAdaptiveBackground(t *testing.T) {
	markdown := strings.Join([]string{
		"```go",
		`fmt.Println("tildewire")`,
		"```",
	}, "\n")

	dark, err := Render(markdown, Options{Style: "dark", Width: 72, DarkBackground: true})
	if err != nil {
		t.Fatalf("render dark markdown: %v", err)
	}
	light, err := Render(markdown, Options{Style: "dark", Width: 72, DarkBackground: false})
	if err != nil {
		t.Fatalf("render light markdown: %v", err)
	}
	if dark == light {
		t.Fatalf("adaptive code block backgrounds should differ")
	}
	for _, rendered := range []string{dark, light} {
		if !strings.Contains(rendered, "48;") {
			t.Fatalf("code block should include a background color:\n%q", rendered)
		}
	}
}

func TestCodeBlockPaletteSlightlyDeepensTerminalBackground(t *testing.T) {
	base := color.RGBA{R: 32, G: 32, B: 44, A: 255}
	palette := codeBlockPaletteFor(Options{DarkBackground: true, TerminalBackground: base})
	body := rgba8(palette.background)
	header := rgba8(palette.headerBackground)

	if body.R >= base.R || body.G >= base.G || body.B >= base.B {
		t.Fatalf("body background should be slightly darker than terminal background: base=%+v body=%+v", base, body)
	}
	if base.R-body.R > 4 || base.G-body.G > 4 || base.B-body.B > 5 {
		t.Fatalf("body background should stay close to terminal background: base=%+v body=%+v", base, body)
	}
	if header.R > base.R || header.G > base.G || header.B > base.B {
		t.Fatalf("header background should not be lighter than terminal background: base=%+v header=%+v", base, header)
	}
	if header.R < body.R || header.G < body.G || header.B < body.B {
		t.Fatalf("header background should be no darker than body background: header=%+v body=%+v", header, body)
	}
}

func TestCodeBlockPaletteUsesReadableTextOnLightBackground(t *testing.T) {
	base := color.RGBA{R: 245, G: 245, B: 245, A: 255}
	palette := codeBlockPaletteFor(Options{
		DarkBackground:     false,
		TerminalBackground: base,
		Theme:              Theme{CodeText: "#CDD6F4"},
	})
	background := rgba8(palette.background)
	bodyText := rgba8(palette.bodyText)

	if background.R < 220 || background.G < 220 || background.B < 220 {
		t.Fatalf("light terminal code background should stay light, got %+v", background)
	}
	if bodyText.R > 80 || bodyText.G > 80 || bodyText.B > 90 {
		t.Fatalf("light terminal code text should stay dark, got %+v", bodyText)
	}
}

func TestRenderInlineCodeUsesLightBackgroundOnLightTerminal(t *testing.T) {
	rendered, err := Render("Use `/understand-knowledge` and `index.md`.", Options{
		Style:              "dark",
		Width:              80,
		DarkBackground:     false,
		TerminalBackground: color.RGBA{R: 245, G: 245, B: 245, A: 255},
		Theme:              Theme{CodeText: "#CDD6F4"},
	})
	if err != nil {
		t.Fatalf("render markdown: %v", err)
	}

	if strings.Contains(rendered, "48;5;236") || strings.Contains(rendered, "48;2;32;32;44") {
		t.Fatalf("inline code should not use dark background on light terminal:\n%q", rendered)
	}
	if !strings.Contains(rendered, "48;2;230;230;230") {
		t.Fatalf("inline code should use adaptive light background:\n%q", rendered)
	}
	if strings.Contains(rendered, "38;2;205;214;244") {
		t.Fatalf("inline code should not use low-contrast dark-theme text on light terminal:\n%q", rendered)
	}
}

func TestRenderMultilineHTMLTextParagraphAsMarkdownText(t *testing.T) {
	markdown := strings.Join([]string{
		`<p align="center">`,
		`  <strong>Turn any codebase, knowledge base, or docs into an interactive knowledge graph you can explore, search, and ask questions about.</strong>`,
		`  <br />`,
		`  <em>Works with Claude Code, Codex, Cursor, Copilot, Gemini CLI, and more.</em>`,
		`</p>`,
	}, "\n")

	rendered, err := Render(markdown, Options{Style: "dark", Width: 96})
	if err != nil {
		t.Fatalf("render markdown: %v", err)
	}
	visible := ansi.Strip(rendered)
	for _, want := range []string{"Turn any codebase", "interactive knowledge graph", "Works with Claude Code"} {
		if !strings.Contains(visible, want) {
			t.Fatalf("rendered HTML paragraph missing %q:\n%s", want, visible)
		}
	}
	for _, forbidden := range []string{"<p", "<strong", "<br", "<em", "+---", "|Turn any"} {
		if strings.Contains(visible, forbidden) {
			t.Fatalf("rendered HTML paragraph leaked %q:\n%s", forbidden, visible)
		}
	}
}

func TestRenderLinkedHTMLImageLineAsImageSegment(t *testing.T) {
	markdown := `<p align="center">
  <a href="https://trendshift.io/repositories/23482" target="_blank"><img src="https://trendshift.io/api/badge/repositories/23482" alt="Lum1104%2FUnderstand-Anything | Trendshift" style="width: 250px; height: 55px;" width="250" height="55"/></a>
</p>`

	rendered, err := Render(markdown, Options{
		Style:         "dark",
		Width:         96,
		ImageSegments: false,
		ImageMode:     config.MarkdownImagePreviewOff,
	})
	if err != nil {
		t.Fatalf("render markdown: %v", err)
	}
	visible := ansi.Strip(rendered)
	if !strings.Contains(visible, "Image: Lum1104%2FUnderstand-Anything | Trendshift") {
		t.Fatalf("rendered linked HTML image missing placeholder:\n%s", visible)
	}
	for _, forbidden := range []string{"<p", "<a", "<img", "+---", "|Lum1104"} {
		if strings.Contains(visible, forbidden) {
			t.Fatalf("rendered linked HTML image leaked %q:\n%s", forbidden, visible)
		}
	}
}

func TestRenderGitHubStyleHTMLHeaderWithoutRawHTMLBlocks(t *testing.T) {
	markdown := strings.Join([]string{
		`<h1 align="center">Understand Anything</h1>`,
		``,
		`<p align="center">`,
		`  <strong>Turn any codebase, knowledge base, or docs into an interactive knowledge graph you can explore, search, and ask questions about.</strong>`,
		`  <br />`,
		`  <em>Works with Claude Code, Codex, Cursor, Copilot, Gemini CLI, and more.</em>`,
		`</p>`,
		``,
		`<p align="center">`,
		`  <a href="README.md">English</a> | <a href="READMEs/README.zh-CN.md">简体中文</a> | <a href="READMEs/README.ja-JP.md">日本語</a>`,
		`</p>`,
	}, "\n")

	rendered, err := Render(markdown, Options{Style: "dark", Width: 96})
	if err != nil {
		t.Fatalf("render markdown: %v", err)
	}
	visible := ansi.Strip(rendered)
	for _, want := range []string{"Understand Anything", "Turn any codebase", "Works with Claude Code", "English", "简体中文", "日本語"} {
		if !strings.Contains(visible, want) {
			t.Fatalf("rendered GitHub-style header missing %q:\n%s", want, visible)
		}
	}
	for _, forbidden := range []string{"<h1", "<p", "<a href", "<strong", "<em", "+---"} {
		if strings.Contains(visible, forbidden) {
			t.Fatalf("rendered GitHub-style header leaked %q:\n%s", forbidden, visible)
		}
	}
}

func TestCodeBlockPaletteUsesTableBorderColor(t *testing.T) {
	palette := codeBlockPaletteFor(Options{
		DarkBackground:     true,
		TerminalBackground: color.RGBA{R: 32, G: 32, B: 44, A: 255},
	})
	if !reflect.DeepEqual(rgba8(palette.border), rgba8(markdownTableBorderColor)) {
		t.Fatalf("code block border = %+v, want table border %+v", rgba8(palette.border), rgba8(markdownTableBorderColor))
	}
}

func TestMarkdownThemeControlsTableDividerAndCodeAccent(t *testing.T) {
	theme := Theme{
		Accent:   "#FF0000",
		Muted:    "#00FF00",
		Text:     "#F8F8F2",
		CodeText: "#E5E7EB",
	}
	tableMarkdown := strings.Join([]string{
		"| What | How |",
		"| --- | --- |",
		"| Theme | Accent |",
	}, "\n")

	renderedTable, err := Render(tableMarkdown, Options{Style: "dark", Width: 64, Theme: theme})
	if err != nil {
		t.Fatalf("render table: %v", err)
	}
	if !strings.Contains(renderedTable, "255;0;0") {
		t.Fatalf("table should use theme accent color:\n%q", renderedTable)
	}

	renderedDivider, err := Render("---", Options{Style: "dark", Width: 40, Theme: theme})
	if err != nil {
		t.Fatalf("render divider: %v", err)
	}
	if !strings.Contains(renderedDivider, "255;0;0") {
		t.Fatalf("divider should use theme accent color:\n%q", renderedDivider)
	}

	palette := codeBlockPaletteFor(Options{
		DarkBackground:     true,
		TerminalBackground: color.RGBA{R: 32, G: 32, B: 44, A: 255},
		Theme:              theme,
	})
	if got := rgba8(palette.border); got != (color.RGBA{R: 255, G: 0, B: 0, A: 255}) {
		t.Fatalf("code block border = %+v, want theme accent", got)
	}
}

func TestRenderThematicBreakSeparator(t *testing.T) {
	markdown := strings.Join([]string{
		"Before",
		"",
		"---",
		"",
		"After",
	}, "\n")

	rendered, err := Render(markdown, Options{Style: "dark", Width: 40})
	if err != nil {
		t.Fatalf("render markdown: %v", err)
	}
	visible := ansi.Strip(rendered)
	for _, want := range []string{"Before", "After", strings.Repeat("─", 16)} {
		if !strings.Contains(visible, want) {
			t.Fatalf("rendered divider missing %q:\n%s", want, visible)
		}
	}
	if strings.Contains(visible, "\n---\n") {
		t.Fatalf("raw thematic break leaked:\n%s", visible)
	}
}

func TestCodeBlocksExtractText(t *testing.T) {
	blocks := CodeBlocks(strings.Join([]string{
		"before",
		"```go",
		`fmt.Println("tildewire")`,
		"```",
	}, "\n"))
	if len(blocks) != 1 {
		t.Fatalf("blocks = %+v, want one", blocks)
	}
	if got := blocks[0].Text(); got != `fmt.Println("tildewire")` {
		t.Fatalf("code block text = %q", got)
	}
}

func TestRenderImageSegmentUsesPreviewStateBySource(t *testing.T) {
	ref := ImageRef{
		Alt: "Preview",
		Src: "assets/preview.png",
		URL: "https://github.com/owner/repo/blob/main/assets/preview.png",
	}
	rendered, err := Render("![Preview](assets/preview.png)", Options{
		Style:         "dark",
		Width:         24,
		BaseURL:       "https://github.com/owner/repo/blob/main/README.md",
		ImageSegments: true,
		ImageMode:     config.MarkdownImagePreviewAuto,
		Images: map[ImageKey]ImageState{
			{URL: "https://raw.githubusercontent.com/owner/repo/main/assets/preview.png", Mode: config.MarkdownImagePreviewHalfblocks, Width: 24}: {
				Src:     ref.Src,
				URL:     "https://raw.githubusercontent.com/owner/repo/main/assets/preview.png",
				Content: "pixels-a\npixels-b",
				Backend: "halfblocks",
			},
		},
	})
	if err != nil {
		t.Fatalf("render markdown: %v", err)
	}
	visible := ansi.Strip(rendered)
	if strings.Contains(visible, "Image: Preview") {
		t.Fatalf("completed preview should hide image label:\n%s", visible)
	}
	if !strings.Contains(visible, "pixels-a") || !strings.Contains(visible, "pixels-b") {
		t.Fatalf("rendered image content missing:\n%s", visible)
	}
}

func TestRenderImageOnlyHTMLWrappersWithoutImageSegments(t *testing.T) {
	markdown := strings.Join([]string{
		`<p align="center"><a href="https://tasteskill.dev" title="Taste Skill - tasteskill.dev"><img src="assets/taste-skill-logo.webp" width="80" height="80" alt="Taste Skill" /></a></p>`,
		"",
		`<p align="center"><a href="https://tasteskill.dev"><img src="https://img.shields.io/badge/OPEN-tasteskill.dev-%23a855f7?style=for-the-badge&labelColor=%230f172a" alt="Open tasteskill.dev" /></a></p>`,
	}, "\n")

	rendered, err := Render(markdown, Options{
		Style:   "dark",
		Width:   96,
		BaseURL: "https://raw.githubusercontent.com/Leonxlnx/taste-skill/refs/heads/main/README.md",
	})
	if err != nil {
		t.Fatalf("render markdown: %v", err)
	}
	visible := ansi.Strip(rendered)
	for _, want := range []string{"Image: Taste Skill", "Image: Open tasteskill.dev"} {
		if !strings.Contains(visible, want) {
			t.Fatalf("rendered image-only HTML wrapper missing %q:\n%s", want, visible)
		}
	}
	for _, forbidden := range []string{"+---", "|T a s t e", "<img", "<a href"} {
		if strings.Contains(visible, forbidden) {
			t.Fatalf("rendered image-only HTML wrapper leaked %q:\n%s", forbidden, visible)
		}
	}
}

func TestRenderMultilineImageOnlyHTMLWrapperWithoutImageSegments(t *testing.T) {
	markdown := strings.Join([]string{
		`<p align="center">`,
		`  <a href="https://tasteskill.dev" title="Taste Skill - tasteskill.dev">`,
		`    <img src="assets/taste-skill-logo.webp" width="80" height="80" alt="Taste Skill" />`,
		`  </a>`,
		`</p>`,
	}, "\n")

	rendered, err := Render(markdown, Options{
		Style:   "dark",
		Width:   96,
		BaseURL: "https://raw.githubusercontent.com/Leonxlnx/taste-skill/refs/heads/main/README.md",
	})
	if err != nil {
		t.Fatalf("render markdown: %v", err)
	}
	visible := ansi.Strip(rendered)
	if !strings.Contains(visible, "Image: Taste Skill") {
		t.Fatalf("rendered multiline image-only HTML wrapper missing placeholder:\n%s", visible)
	}
	for _, forbidden := range []string{"<p", "<a href", "</a>", "</p>", "+---", "|T a s t e"} {
		if strings.Contains(visible, forbidden) {
			t.Fatalf("rendered multiline image-only HTML wrapper leaked %q:\n%s", forbidden, visible)
		}
	}
}

func TestRawLineInfoRoundTrips(t *testing.T) {
	raw := "\x1b_Ga=T;RAW\x1b\\"
	line := MakeRawLine(raw, 20)
	got, columns, ok := RawLineInfo(line)
	if !ok {
		t.Fatal("raw line was not recognized")
	}
	if got != raw || columns != 20 {
		t.Fatalf("raw line = %q %d, want %q 20", got, columns, raw)
	}
}

func TestImagePreviewModes(t *testing.T) {
	for _, mode := range []string{
		config.MarkdownImagePreviewAuto,
		config.MarkdownImagePreviewKitty,
		config.MarkdownImagePreviewITerm,
		config.MarkdownImagePreviewSixel,
	} {
		if got := ImageRenderMode(mode); got != tuiimage.RenderModePreviewPane {
			t.Fatalf("render mode for %q = %q, want %q", mode, got, tuiimage.RenderModePreviewPane)
		}
	}
	if got := ImageRenderMode(config.MarkdownImagePreviewHalfblocks); got != tuiimage.RenderModeScrollableInline {
		t.Fatalf("render mode for halfblocks = %q, want %q", got, tuiimage.RenderModeScrollableInline)
	}
}

func rgba8(value color.Color) color.RGBA {
	return color.RGBAModel.Convert(value).(color.RGBA)
}
