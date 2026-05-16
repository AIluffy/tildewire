package image

import "testing"

func TestComputeVisibleImagesFullyVisibleImage(t *testing.T) {
	items := []ImageLayoutItem{{ID: "one", Path: "one.png", ContentY: 3, WidthCells: 10, HeightCells: 4}}

	visible := ComputeVisibleImages(items, 0, 10)

	if len(visible) != 1 {
		t.Fatalf("visible images = %d, want 1", len(visible))
	}
	got := visible[0]
	if got.DrawY != 3 || got.Rect != (CellRect{X: 0, Y: 3, Width: 10, Height: 4}) || !got.FullyVisible {
		t.Fatalf("visible image = %#v", got)
	}
}

func TestComputeVisibleImagesPartiallyVisibleTopClipped(t *testing.T) {
	items := []ImageLayoutItem{{ID: "one", Path: "one.png", ContentY: 2, WidthCells: 10, HeightCells: 5}}

	visible := ComputeVisibleImages(items, 4, 6)

	if len(visible) != 1 {
		t.Fatalf("visible images = %d, want 1", len(visible))
	}
	got := visible[0]
	wantCrop := &PixelRect{X: 0, Y: 2, Width: 10, Height: 5}
	if got.DrawY != 0 || got.Rect != (CellRect{X: 0, Y: 0, Width: 10, Height: 3}) || got.FullyVisible || got.Crop == nil || *got.Crop != *wantCrop {
		t.Fatalf("top-clipped visible image = %#v", got)
	}
}

func TestComputeVisibleImagesPartiallyVisibleBottomClipped(t *testing.T) {
	items := []ImageLayoutItem{{ID: "one", Path: "one.png", ContentY: 4, WidthCells: 10, HeightCells: 5}}

	visible := ComputeVisibleImages(items, 0, 6)

	if len(visible) != 1 {
		t.Fatalf("visible images = %d, want 1", len(visible))
	}
	got := visible[0]
	wantCrop := &PixelRect{X: 0, Y: 0, Width: 10, Height: 5}
	if got.DrawY != 4 || got.Rect != (CellRect{X: 0, Y: 4, Width: 10, Height: 2}) || got.FullyVisible || got.Crop == nil || *got.Crop != *wantCrop {
		t.Fatalf("bottom-clipped visible image = %#v", got)
	}
}

func TestComputeVisibleImagesSkipsInvisibleAboveViewport(t *testing.T) {
	items := []ImageLayoutItem{{ID: "one", Path: "one.png", ContentY: 0, WidthCells: 10, HeightCells: 3}}

	visible := ComputeVisibleImages(items, 5, 4)

	if len(visible) != 0 {
		t.Fatalf("visible images = %#v, want none", visible)
	}
}

func TestComputeVisibleImagesSkipsInvisibleBelowViewport(t *testing.T) {
	items := []ImageLayoutItem{{ID: "one", Path: "one.png", ContentY: 10, WidthCells: 10, HeightCells: 3}}

	visible := ComputeVisibleImages(items, 0, 4)

	if len(visible) != 0 {
		t.Fatalf("visible images = %#v, want none", visible)
	}
}

func TestComputeVisibleImagesIgnoresZeroSizeImage(t *testing.T) {
	items := []ImageLayoutItem{
		{ID: "zero-width", Path: "one.png", ContentY: 0, WidthCells: 0, HeightCells: 3},
		{ID: "zero-height", Path: "two.png", ContentY: 0, WidthCells: 10, HeightCells: 0},
	}

	visible := ComputeVisibleImages(items, 0, 4)

	if len(visible) != 0 {
		t.Fatalf("visible images = %#v, want none", visible)
	}
}

func TestComputeVisibleImagesHandlesNegativeViewportOffset(t *testing.T) {
	items := []ImageLayoutItem{{ID: "one", Path: "one.png", ContentY: 0, WidthCells: 8, HeightCells: 2}}

	visible := ComputeVisibleImages(items, -3, 6)

	if len(visible) != 1 {
		t.Fatalf("visible images = %d, want 1", len(visible))
	}
	got := visible[0]
	if got.DrawY != 3 || got.Rect != (CellRect{X: 0, Y: 3, Width: 8, Height: 2}) || !got.FullyVisible {
		t.Fatalf("negative viewport visible image = %#v", got)
	}
}
