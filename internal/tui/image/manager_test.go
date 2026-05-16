package image

import (
	"context"
	"errors"
	stdimage "image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestDetectStableProtocolGraphicsDisabled(t *testing.T) {
	got := DetectStableProtocol(ImageManagerConfig{EnableGraphicsProtocols: false}, RenderModePreviewPane)

	if got != ProtocolHalfblocks {
		t.Fatalf("protocol = %s, want %s", got, ProtocolHalfblocks)
	}
}

func TestDetectStableProtocolScrollableInlineDefaultsToHalfblocks(t *testing.T) {
	restore := replaceDefaultProbe(fakeTerminalProbe{
		tty:       true,
		utf8:      true,
		protocols: []ImageProtocol{ProtocolKitty, ProtocolHalfblocks},
		metrics:   TerminalMetrics{FontWidth: 8, FontHeight: 16},
	})
	defer restore()

	got := DetectStableProtocol(ImageManagerConfig{EnableGraphicsProtocols: true}, RenderModeScrollableInline)

	if got != ProtocolHalfblocks {
		t.Fatalf("protocol = %s, want %s", got, ProtocolHalfblocks)
	}
}

func TestDetectStableProtocolTmuxDefaultsToHalfblocks(t *testing.T) {
	restore := replaceDefaultProbe(fakeTerminalProbe{
		env:       map[string]string{"TMUX": "/tmp/tmux"},
		tty:       true,
		utf8:      true,
		protocols: []ImageProtocol{ProtocolKitty, ProtocolHalfblocks},
		metrics:   TerminalMetrics{FontWidth: 8, FontHeight: 16},
	})
	defer restore()

	got := DetectStableProtocol(ImageManagerConfig{EnableGraphicsProtocols: true}, RenderModePreviewPane)

	if got != ProtocolHalfblocks {
		t.Fatalf("protocol = %s, want %s", got, ProtocolHalfblocks)
	}
}

func TestDetectStableProtocolPreferredProtocolUnavailableFallsBack(t *testing.T) {
	restore := replaceDefaultProbe(fakeTerminalProbe{
		tty:       true,
		utf8:      true,
		protocols: []ImageProtocol{ProtocolHalfblocks},
		metrics:   TerminalMetrics{FontWidth: 8, FontHeight: 16},
	})
	defer restore()

	got := DetectStableProtocol(ImageManagerConfig{
		PreferredProtocol:       ProtocolKitty,
		EnableGraphicsProtocols: true,
	}, RenderModePreviewPane)

	if got != ProtocolHalfblocks {
		t.Fatalf("protocol = %s, want %s", got, ProtocolHalfblocks)
	}
}

func TestDetectStableProtocolCIDumbTerminalFallsBack(t *testing.T) {
	restore := replaceDefaultProbe(fakeTerminalProbe{
		env:       map[string]string{"CI": "true", "TERM": "dumb"},
		tty:       true,
		utf8:      true,
		protocols: []ImageProtocol{ProtocolKitty, ProtocolHalfblocks},
		metrics:   TerminalMetrics{FontWidth: 8, FontHeight: 16},
	})
	defer restore()

	got := DetectStableProtocol(ImageManagerConfig{EnableGraphicsProtocols: true}, RenderModePreviewPane)

	if got != ProtocolHalfblocks {
		t.Fatalf("protocol = %s, want %s", got, ProtocolHalfblocks)
	}
}

func TestImageManagerAcceptIgnoresStaleRenderResult(t *testing.T) {
	manager := newImageManager(ImageManagerConfig{}, withTerminalProbe(fakeTerminalProbe{tty: true, utf8: true}))
	manager.currentVersion = 2
	req := ImageRequest{ID: "preview", Path: "preview.png", Rect: CellRect{Width: 12, Height: 4}, AltText: "Preview"}

	accepted := manager.Accept(RenderedMsg{
		Version: 1,
		Image:   RenderedImage{ID: req.ID, Protocol: ProtocolHalfblocks, Cells: "stale", Rect: req.Rect, WidthCells: 12, HeightCells: 4},
		Request: req,
	})

	if accepted {
		t.Fatal("stale render result was accepted")
	}
	if got := manager.Rendered(req); strings.Contains(got.Cells, "stale") {
		t.Fatalf("stale image leaked into cache: %#v", got)
	}
}

func TestImageManagerAcceptNewestRenderResultWins(t *testing.T) {
	manager := newImageManager(ImageManagerConfig{}, withTerminalProbe(fakeTerminalProbe{tty: true, utf8: true}))
	req := ImageRequest{ID: "preview", Path: writeManagerPNG(t, "preview.png"), Rect: CellRect{Width: 12, Height: 4}, AltText: "Preview"}
	manager.currentVersion = 2

	if !manager.Accept(RenderedMsg{
		Version: 2,
		Image:   RenderedImage{ID: req.ID, Protocol: ProtocolHalfblocks, Cells: "newest", Rect: req.Rect, WidthCells: 12, HeightCells: 4},
		Request: req,
	}) {
		t.Fatal("newest render result was rejected")
	}
	if manager.Accept(RenderedMsg{
		Version: 1,
		Image:   RenderedImage{ID: req.ID, Protocol: ProtocolHalfblocks, Cells: "stale", Rect: req.Rect, WidthCells: 12, HeightCells: 4},
		Request: req,
	}) {
		t.Fatal("older render result should be ignored after newest result")
	}
	if got := manager.Rendered(req); got.Cells != "newest" {
		t.Fatalf("rendered cells = %q, want newest", got.Cells)
	}
}

func TestRenderImageAsyncDecodeFailureReturnsPlaceholder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.png")
	if err := os.WriteFile(path, []byte("not an image"), 0o644); err != nil {
		t.Fatalf("write corrupt image: %v", err)
	}
	manager := newImageManager(ImageManagerConfig{},
		withTerminalProbe(fakeTerminalProbe{tty: true, utf8: true, protocols: []ImageProtocol{ProtocolHalfblocks}, metrics: TerminalMetrics{FontWidth: 8, FontHeight: 16}}),
		withImageBackend(&fakeImageBackend{}),
	)
	req := ImageRequest{ID: "bad", Path: path, Rect: CellRect{Width: 16, Height: 4}, AltText: "Broken image"}

	msg := renderCommandMsg(t, manager.RenderImageAsync(req))

	if msg.Err == nil || msg.Err.Kind != ErrDecodeFailed {
		t.Fatalf("error = %#v, want decode failure", msg.Err)
	}
	if msg.Image.Protocol != ProtocolHalfblocks || !strings.Contains(msg.Image.Cells, "Broken image") {
		t.Fatalf("decode failure image = %#v", msg.Image)
	}
}

func TestRenderImageAsyncProtocolFailureFallsBackToHalfblocks(t *testing.T) {
	path := writeManagerPNG(t, "fallback.png")
	backend := &fakeImageBackend{
		failures: map[ImageProtocol]error{ProtocolKitty: errors.New("kitty failed")},
		cells:    map[ImageProtocol]string{ProtocolHalfblocks: "halfblocks"},
	}
	manager := newImageManager(ImageManagerConfig{
		PreferredProtocol:       ProtocolKitty,
		EnableGraphicsProtocols: true,
		MaxRenderConcurrency:    1,
	},
		withTerminalProbe(fakeTerminalProbe{tty: true, utf8: true, protocols: []ImageProtocol{ProtocolKitty, ProtocolHalfblocks}, metrics: TerminalMetrics{FontWidth: 8, FontHeight: 16}}),
		withImageBackend(backend),
	)
	req := ImageRequest{ID: "fallback", Path: path, Rect: CellRect{Width: 16, Height: 4}, AltText: "Preview"}

	msg := renderCommandMsg(t, manager.RenderImageAsync(req))

	if msg.Err != nil {
		t.Fatalf("render err = %#v, want successful fallback", msg.Err)
	}
	if msg.FallbackFrom != ProtocolKitty || msg.Image.Protocol != ProtocolHalfblocks || msg.Image.Cells != "halfblocks" {
		t.Fatalf("fallback msg = %#v", msg)
	}
}

func TestImageManagerRepeatedProtocolFailureDisablesGraphicsForSession(t *testing.T) {
	path := writeManagerPNG(t, "disable.png")
	backend := &fakeImageBackend{
		failures: map[ImageProtocol]error{ProtocolKitty: errors.New("kitty failed")},
		cells:    map[ImageProtocol]string{ProtocolHalfblocks: "halfblocks"},
	}
	manager := newImageManager(ImageManagerConfig{
		PreferredProtocol:       ProtocolKitty,
		EnableGraphicsProtocols: true,
		MaxRenderConcurrency:    1,
	},
		withTerminalProbe(fakeTerminalProbe{tty: true, utf8: true, protocols: []ImageProtocol{ProtocolKitty, ProtocolHalfblocks}, metrics: TerminalMetrics{FontWidth: 8, FontHeight: 16}}),
		withImageBackend(backend),
	)
	req := ImageRequest{ID: "disable", Path: path, Rect: CellRect{Width: 16, Height: 4}, AltText: "Preview"}

	first := renderCommandMsg(t, manager.RenderImageAsync(req))
	if !manager.Accept(first) {
		t.Fatal("first fallback result was not accepted")
	}
	second := renderCommandMsg(t, manager.RenderImageAsync(req))
	if !manager.Accept(second) {
		t.Fatal("second fallback result was not accepted")
	}

	if got := manager.ProtocolFor(RenderModePreviewPane); got != ProtocolHalfblocks {
		t.Fatalf("protocol after repeated failures = %s, want %s", got, ProtocolHalfblocks)
	}
}

func TestDitherForProtocolEnablesHalfblocksFallbackQuality(t *testing.T) {
	if !ditherForProtocol(ProtocolHalfblocks) {
		t.Fatal("halfblocks fallback should enable dithering")
	}
	for _, protocol := range []ImageProtocol{ProtocolKitty, ProtocolITerm2, ProtocolSixel} {
		if ditherForProtocol(protocol) {
			t.Fatalf("%s should not force dithering", protocol)
		}
	}
}

type fakeTerminalProbe struct {
	env       map[string]string
	tty       bool
	utf8      bool
	protocols []ImageProtocol
	metrics   TerminalMetrics
}

func (p fakeTerminalProbe) Env(key string) string {
	return p.env[key]
}

func (p fakeTerminalProbe) IsStdoutTTY() bool {
	return p.tty
}

func (p fakeTerminalProbe) LocaleIsUTF8() bool {
	return p.utf8
}

func (p fakeTerminalProbe) Protocols() []ImageProtocol {
	if len(p.protocols) == 0 {
		return []ImageProtocol{ProtocolHalfblocks}
	}
	return append([]ImageProtocol(nil), p.protocols...)
}

func (p fakeTerminalProbe) Metrics() TerminalMetrics {
	if p.metrics.FontWidth <= 0 || p.metrics.FontHeight <= 0 {
		return TerminalMetrics{FontWidth: 8, FontHeight: 16}
	}
	return p.metrics
}

type fakeImageBackend struct {
	failures map[ImageProtocol]error
	cells    map[ImageProtocol]string
}

func (b *fakeImageBackend) Render(ctx context.Context, _ stdimage.Image, req ImageRequest, protocol ImageProtocol, _ TerminalMetrics) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := b.failures[protocol]; err != nil {
		return "", err
	}
	if cells := b.cells[protocol]; cells != "" {
		return cells, nil
	}
	return string(protocol) + ":" + req.ID, nil
}

func replaceDefaultProbe(probe terminalProbe) func() {
	defaultProbeMu.Lock()
	old := defaultProbe
	defaultProbe = probe
	defaultProbeMu.Unlock()
	return func() {
		defaultProbeMu.Lock()
		defaultProbe = old
		defaultProbeMu.Unlock()
	}
}

func renderCommandMsg(t *testing.T, cmd tea.Cmd) RenderedMsg {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected render command")
	}
	msg, ok := cmd().(RenderedMsg)
	if !ok {
		t.Fatalf("command returned %T, want RenderedMsg", msg)
	}
	return msg
}

func writeManagerPNG(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	img := stdimage.NewRGBA(stdimage.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: uint8(30 * x), G: uint8(30 * y), B: 160, A: 255})
		}
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create png: %v", err)
	}
	defer file.Close()
	if err := png.Encode(file, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return path
}
