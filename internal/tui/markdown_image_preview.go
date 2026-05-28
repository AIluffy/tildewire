package tui

import (
	"bytes"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	kittygfx "github.com/charmbracelet/x/ansi/kitty"

	"github.com/AIluffy/tildewire/internal/config"
	"github.com/AIluffy/tildewire/internal/tui/githubreadme"
	tuiimage "github.com/AIluffy/tildewire/internal/tui/image"
	md "github.com/AIluffy/tildewire/internal/tui/markdown"
)

const (
	markdownImageMaxBytes     = md.MaxImageBytes
	detailRawLinePrefix       = md.RawLinePrefix
	detailRawImageRedrawDelay = 100 * time.Millisecond
	detailImageMaxHeight      = 40
)

type markdownImageRef = md.ImageRef
type markdownImagePreviewRequest = md.ImageRequest
type markdownImagePreviewResult = md.ImageResult
type markdownImagePreviewKey = md.ImageKey
type markdownImagePreviewState = md.ImageState
type markdownImagePreviewer = md.Previewer

type detailRawImageDraw struct {
	row     int
	col     int
	content string
}

type detailRawImageRedrawMsg struct {
	drawID       int
	itemID       string
	detailOffset int
	imageVersion int
}

func newTerminalMarkdownImagePreviewer(cacheDir string) markdownImagePreviewer {
	return md.NewTerminalImagePreviewer(cacheDir)
}

func (m *Model) queueDetailImagePreviewCmds() tea.Cmd {
	if !m.overlayIs(overlayDetail) || normalizeMarkdownImagePreviewMode(m.config.MarkdownImagePreview) == config.MarkdownImagePreviewOff {
		return nil
	}
	section, ok := githubreadme.Section(m.itemDetail)
	if !ok {
		return nil
	}
	refs := githubReadmeImageRefs(section.Body, section.URL)
	if len(refs) == 0 {
		return nil
	}
	if m.imagePreviews == nil {
		m.imagePreviews = make(map[markdownImagePreviewKey]markdownImagePreviewState)
	}
	width := min(m.detailContentWidth(), 96)
	mode := scrollableMarkdownImagePreviewMode(m.config.MarkdownImagePreview)
	maxHeight := detailImagePreviewHeight(width, m.detailVisibleHeight())
	cmds := make([]tea.Cmd, 0, len(refs))
	for _, ref := range refs {
		key := markdownImagePreviewKey{URL: ref.URL, Mode: mode, Width: width}
		state, ok := m.imagePreviews[key]
		if ok && (state.Loading || state.Content != "" || state.Err != "") {
			continue
		}
		m.imagePreviews[key] = markdownImagePreviewState{
			Alt:     ref.Alt,
			Src:     ref.Src,
			URL:     ref.URL,
			Loading: true,
		}
		m.detailImageVersion++
		cmds = append(cmds, m.markdownImagePreviewCmd(m.detailEntryID, key, markdownImagePreviewRequest{
			URL:       ref.URL,
			Alt:       ref.Alt,
			Mode:      mode,
			CacheDir:  m.config.CacheDir,
			Width:     width,
			MaxHeight: maxHeight,
		}))
	}
	if len(cmds) == 0 {
		return nil
	}
	m.clearDetailContentCache()
	return tea.Batch(cmds...)
}

func detailImagePreviewHeight(width, visibleHeight int) int {
	height := max(4, width/2)
	if visibleHeight > 4 {
		height = min(height, max(4, visibleHeight-3))
	}
	return clamp(height, 4, detailImageMaxHeight)
}

func (m Model) renderMarkdownImageSegment(ref markdownImageRef, width int) string {
	mode := scrollableMarkdownImagePreviewMode(m.config.MarkdownImagePreview)
	key := markdownImagePreviewKey{URL: ref.URL, Mode: mode, Width: min(width, 96)}
	state, ok := m.imagePreviews[key]
	return md.RenderImageSegment(ref, width, m.config.MarkdownImagePreview, state, ok, m.styles.markdown)
}

func (m Model) drawDetailRawImagesCmd() tea.Cmd {
	sequence := m.detailRawImageDrawSequence()
	if sequence == "" {
		return nil
	}
	return tea.Raw(sequence)
}

func (m *Model) scrollDetailRawImagesCmd(delta int) tea.Cmd {
	if !m.scrollDetail(delta) {
		return nil
	}
	return batchCommands(m.clearDetailRawImagesCmd(), m.scheduleDetailRawImagesDrawCmd())
}

func (m Model) clearDetailRawImagesCmd() tea.Cmd {
	if !m.detailHasKittyRawImages() {
		return nil
	}
	sequence := kittyRawImageClearSequence()
	if sequence == "" {
		return nil
	}
	return tea.Raw(sequence)
}

func (m Model) detailHasKittyRawImages() bool {
	for _, state := range m.imagePreviews {
		if state.Raw && state.Content != "" && state.Backend == string(tuiimage.ProtocolKitty) {
			return true
		}
	}
	return false
}

func kittyRawImageClearSequence() string {
	var sequence bytes.Buffer
	if err := kittygfx.EncodeGraphics(&sequence, nil, &kittygfx.Options{
		Action: kittygfx.Delete,
		Quite:  2,
		Delete: kittygfx.DeleteAll,
	}); err != nil {
		return ""
	}
	return sequence.String()
}

func (m *Model) scheduleDetailRawImagesDrawCmd() tea.Cmd {
	if len(m.visibleDetailRawImageDraws()) == 0 {
		return nil
	}
	m.detailRawImageDrawID++
	msg := detailRawImageRedrawMsg{
		drawID:       m.detailRawImageDrawID,
		itemID:       m.detailEntryID,
		detailOffset: m.detailOffset,
		imageVersion: m.detailImageVersion,
	}
	return tea.Tick(detailRawImageRedrawDelay, func(time.Time) tea.Msg {
		return msg
	})
}

func (m Model) detailRawImageDrawSequence() string {
	draws := m.visibleDetailRawImageDraws()
	if len(draws) == 0 {
		return ""
	}
	var sequence strings.Builder
	for _, draw := range draws {
		sequence.WriteString(ansi.SaveCursor)
		sequence.WriteString(ansi.CursorPosition(draw.col, draw.row))
		sequence.WriteString(draw.content)
		sequence.WriteString(ansi.RestoreCursor)
	}
	return sequence.String()
}

func (m Model) visibleDetailRawImageDraws() []detailRawImageDraw {
	if !m.overlayIs(overlayDetail) {
		return nil
	}
	width := m.detailContentWidth()
	lines := m.detailContentLines(width)
	if len(lines) == 0 {
		return nil
	}
	visibleHeight := m.detailVisibleHeight()
	offset := clamp(m.detailOffset, 0, maxDetailOffset(len(lines), visibleHeight))
	end := min(len(lines), offset+visibleHeight)
	if end <= offset {
		return nil
	}
	renderWidth := max(40, m.width)
	indent := max(0, (renderWidth-width)/2)
	draws := make([]detailRawImageDraw, 0)
	for idx := offset; idx < end; idx++ {
		raw, columns, ok := detailRawLineInfo(lines[idx])
		if !ok || raw == "" {
			continue
		}
		columns = clamp(columns, 1, width)
		draws = append(draws, detailRawImageDraw{
			row:     idx - offset + 1,
			col:     indent + 1 + max(0, (width-columns)/2),
			content: raw,
		})
	}
	return draws
}

func normalizeMarkdownImagePreviewMode(mode string) string {
	return md.NormalizeImagePreviewMode(mode)
}

func scrollableMarkdownImagePreviewMode(mode string) string {
	return md.ScrollableImagePreviewMode(mode)
}

func markdownImageRenderMode(mode string) tuiimage.ImageRenderMode {
	return md.ImageRenderMode(mode)
}

func markdownImageProtocol(mode string) tuiimage.ImageProtocol {
	return md.ImageProtocol(mode)
}

func detailRawLine(line string) (string, bool) {
	return md.RawLine(line)
}

func detailRawLineInfo(line string) (string, int, bool) {
	return md.RawLineInfo(line)
}

func githubReadmeImageRefs(markdown string, readmeURL string) []markdownImageRef {
	return githubreadme.ImageRefs(markdown, readmeURL)
}

func resolveGitHubReadmeImageURL(src, readmeURL string) (string, bool) {
	return githubreadme.ResolveImageURL(src, readmeURL)
}
