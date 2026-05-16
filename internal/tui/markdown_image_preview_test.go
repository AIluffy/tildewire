package tui

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/AIluffy/tildewire/internal/config"
	tuiimage "github.com/AIluffy/tildewire/internal/tui/image"
)

func TestTerminalMarkdownImagePreviewFallsBackToHalfblocksWhenGraphicsUnavailable(t *testing.T) {
	imageURL := stubMarkdownImageHTTP(t, 24, 12)
	previewer := newTerminalMarkdownImagePreviewer(t.TempDir())

	result, err := previewer.RenderMarkdownImage(context.Background(), markdownImagePreviewRequest{
		URL:       imageURL,
		Alt:       "Managed preview",
		Mode:      config.MarkdownImagePreviewAuto,
		CacheDir:  t.TempDir(),
		Width:     24,
		MaxHeight: 8,
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Raw {
		t.Fatalf("scrollable markdown preview returned raw protocol content: %+v", result)
	}
	if result.Backend != "halfblocks" {
		t.Fatalf("backend = %q, want halfblocks", result.Backend)
	}
	if strings.TrimSpace(result.Content) == "" {
		t.Fatal("expected stable fallback content")
	}
}

func TestTerminalMarkdownImagePreviewFallsBackForExplicitKittyWhenGraphicsUnavailable(t *testing.T) {
	imageURL := stubMarkdownImageHTTP(t, 24, 12)
	previewer := newTerminalMarkdownImagePreviewer(t.TempDir())

	result, err := previewer.RenderMarkdownImage(context.Background(), markdownImagePreviewRequest{
		URL:       imageURL,
		Alt:       "Kitty requested",
		Mode:      config.MarkdownImagePreviewKitty,
		CacheDir:  t.TempDir(),
		Width:     24,
		MaxHeight: 8,
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Raw || result.Backend != "halfblocks" {
		t.Fatalf("explicit kitty in scrollable content = %+v, want managed halfblocks fallback", result)
	}
}

func TestMarkdownImagePreviewUsesFixedRenderModeForAutoAndExplicitGraphics(t *testing.T) {
	for _, mode := range []string{
		config.MarkdownImagePreviewAuto,
		config.MarkdownImagePreviewKitty,
		config.MarkdownImagePreviewITerm,
		config.MarkdownImagePreviewSixel,
	} {
		if got := markdownImageRenderMode(mode); got != tuiimage.RenderModePreviewPane {
			t.Fatalf("render mode for %q = %q, want %q", mode, got, tuiimage.RenderModePreviewPane)
		}
	}
	if got := markdownImageRenderMode(config.MarkdownImagePreviewHalfblocks); got != tuiimage.RenderModeScrollableInline {
		t.Fatalf("render mode for halfblocks = %q, want %q", got, tuiimage.RenderModeScrollableInline)
	}
}

func TestDetailImagePreviewHeightUsesMoreOfSmallViewport(t *testing.T) {
	if got := detailImagePreviewHeight(80, 16); got != 13 {
		t.Fatalf("preview height = %d, want 13", got)
	}
}

func TestRenderMarkdownImageSegmentRendersOnlyImageWithEqualVerticalMargins(t *testing.T) {
	ref := markdownImageRef{
		Alt: "Preview",
		Src: "assets/preview.png",
		URL: "https://example.test/preview.png",
	}
	width := 24
	model := Model{
		config: config.Config{MarkdownImagePreview: config.MarkdownImagePreviewAuto},
		imagePreviews: map[markdownImagePreviewKey]markdownImagePreviewState{
			{URL: ref.URL, Mode: config.MarkdownImagePreviewHalfblocks, Width: width}: {
				Content: "pixels-a\npixels-b",
				Backend: "halfblocks",
			},
		},
	}

	lines := strings.Split(ansi.Strip(model.renderMarkdownImageSegment(ref, width)), "\n")
	if strings.Contains(strings.Join(lines, "\n"), "Image: Preview") {
		t.Fatalf("image label should not render with a completed preview: %#v", lines)
	}
	if len(lines) != 4 {
		t.Fatalf("image segment too short: %#v", lines)
	}
	if strings.TrimSpace(lines[0]) != "" {
		t.Fatalf("line before image should be blank, got %q in %#v", lines[0], lines)
	}
	if strings.TrimSpace(lines[1]) == "" || strings.TrimSpace(lines[2]) == "" {
		t.Fatalf("image content should render after top margin: %#v", lines)
	}
	if strings.TrimSpace(lines[3]) != "" {
		t.Fatalf("line after image should be blank, got %q in %#v", lines[3], lines)
	}
}

func TestTerminalMarkdownImagePreviewOffReturnsDisabledError(t *testing.T) {
	previewer := newTerminalMarkdownImagePreviewer(t.TempDir())

	_, err := previewer.RenderMarkdownImage(context.Background(), markdownImagePreviewRequest{
		URL:  "https://example.com/preview.png",
		Mode: config.MarkdownImagePreviewOff,
	})

	if err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("error = %v, want disabled error", err)
	}
}

func stubMarkdownImageHTTP(t *testing.T, width, height int) string {
	t.Helper()
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 0x88, A: 0xff})
		}
	}
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	payload := buf.Bytes()
	oldTransport := http.DefaultTransport
	http.DefaultTransport = roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"image/png"}},
			Body:       io.NopCloser(bytes.NewReader(payload)),
			Request:    req,
		}, nil
	})
	t.Cleanup(func() {
		http.DefaultTransport = oldTransport
	})
	return "https://example.test/preview.png"
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
