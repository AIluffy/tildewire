package image

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestImageCacheRenderedHitForSamePathSizeAndProtocol(t *testing.T) {
	path := writeCacheFile(t, "same.txt", "one")
	cache := NewImageCache(4, 1<<20)
	req := ImageRequest{ID: "same", Path: path, Rect: CellRect{Width: 10, Height: 4}}
	metrics := TerminalMetrics{FontWidth: 8, FontHeight: 16}
	rendered := RenderedImage{ID: "same", Protocol: ProtocolHalfblocks, Cells: "cached", Rect: req.Rect, WidthCells: 10, HeightCells: 4}

	if err := cache.PutRendered(req, ProtocolHalfblocks, metrics, rendered); err != nil {
		t.Fatalf("put rendered: %v", err)
	}
	got, ok := cache.GetRendered(req, ProtocolHalfblocks, metrics)
	if !ok {
		t.Fatal("expected rendered cache hit")
	}
	if got.Cells != "cached" || !got.FromCache {
		t.Fatalf("cached image = %#v", got)
	}
}

func TestImageCacheResizeInvalidatesRenderedCache(t *testing.T) {
	path := writeCacheFile(t, "resize.txt", "one")
	cache := NewImageCache(4, 1<<20)
	req := ImageRequest{ID: "resize", Path: path, Rect: CellRect{Width: 10, Height: 4}}

	if err := cache.PutRendered(req, ProtocolHalfblocks, TerminalMetrics{FontWidth: 8, FontHeight: 16}, RenderedImage{ID: "resize", Cells: "old"}); err != nil {
		t.Fatalf("put rendered: %v", err)
	}
	if _, ok := cache.GetRendered(req, ProtocolHalfblocks, TerminalMetrics{FontWidth: 9, FontHeight: 18}); ok {
		t.Fatal("expected rendered cache miss after terminal cell resize")
	}
}

func TestImageCacheProtocolChangeInvalidatesRenderedCache(t *testing.T) {
	path := writeCacheFile(t, "protocol.txt", "one")
	cache := NewImageCache(4, 1<<20)
	req := ImageRequest{ID: "protocol", Path: path, Rect: CellRect{Width: 10, Height: 4}}
	metrics := TerminalMetrics{FontWidth: 8, FontHeight: 16}

	if err := cache.PutRendered(req, ProtocolKitty, metrics, RenderedImage{ID: "protocol", Protocol: ProtocolKitty, Cells: "kitty"}); err != nil {
		t.Fatalf("put rendered: %v", err)
	}
	if _, ok := cache.GetRendered(req, ProtocolHalfblocks, metrics); ok {
		t.Fatal("expected rendered cache miss after protocol change")
	}
}

func TestImageCacheFileChangeInvalidatesRenderedCache(t *testing.T) {
	path := writeCacheFile(t, "mtime.txt", "one")
	cache := NewImageCache(4, 1<<20)
	req := ImageRequest{ID: "mtime", Path: path, Rect: CellRect{Width: 10, Height: 4}}
	metrics := TerminalMetrics{FontWidth: 8, FontHeight: 16}

	if err := cache.PutRendered(req, ProtocolHalfblocks, metrics, RenderedImage{ID: "mtime", Cells: "old"}); err != nil {
		t.Fatalf("put rendered: %v", err)
	}
	if err := os.WriteFile(path, []byte("changed-size"), 0o644); err != nil {
		t.Fatalf("change file: %v", err)
	}
	if err := os.Chtimes(path, time.Now().Add(time.Second), time.Now().Add(time.Second)); err != nil {
		t.Fatalf("change mtime: %v", err)
	}
	if _, ok := cache.GetRendered(req, ProtocolHalfblocks, metrics); ok {
		t.Fatal("expected rendered cache miss after file change")
	}
}

func TestImageCacheLRUEvictionWorks(t *testing.T) {
	cache := NewImageCache(2, 1<<20)
	metrics := TerminalMetrics{FontWidth: 8, FontHeight: 16}
	reqA := ImageRequest{ID: "a", Path: writeCacheFile(t, "a.txt", "a"), Rect: CellRect{Width: 10, Height: 4}}
	reqB := ImageRequest{ID: "b", Path: writeCacheFile(t, "b.txt", "b"), Rect: CellRect{Width: 10, Height: 4}}
	reqC := ImageRequest{ID: "c", Path: writeCacheFile(t, "c.txt", "c"), Rect: CellRect{Width: 10, Height: 4}}

	for _, req := range []ImageRequest{reqA, reqB} {
		if err := cache.PutRendered(req, ProtocolHalfblocks, metrics, RenderedImage{ID: req.ID, Cells: req.ID}); err != nil {
			t.Fatalf("put %s: %v", req.ID, err)
		}
	}
	if _, ok := cache.GetRendered(reqA, ProtocolHalfblocks, metrics); !ok {
		t.Fatal("expected reqA cache hit before eviction")
	}
	if err := cache.PutRendered(reqC, ProtocolHalfblocks, metrics, RenderedImage{ID: "c", Cells: "c"}); err != nil {
		t.Fatalf("put reqC: %v", err)
	}

	if _, ok := cache.GetRendered(reqB, ProtocolHalfblocks, metrics); ok {
		t.Fatal("expected least recently used reqB to be evicted")
	}
	if _, ok := cache.GetRendered(reqA, ProtocolHalfblocks, metrics); !ok {
		t.Fatal("expected recently used reqA to remain cached")
	}
}

func writeCacheFile(t *testing.T, name string, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write cache file: %v", err)
	}
	return path
}
