package image

// ImageLayoutItem is a fixed-height image slot inside scrollable content.
type ImageLayoutItem struct {
	ID          string
	Path        string
	ContentY    int
	WidthCells  int
	HeightCells int
}

// VisibleImage is the viewport-relative placement for a visible image slot.
type VisibleImage struct {
	ID           string
	Path         string
	DrawY        int
	Rect         CellRect
	Crop         *PixelRect
	FullyVisible bool
}

// ComputeVisibleImages returns image slots intersecting a logical viewport.
func ComputeVisibleImages(items []ImageLayoutItem, viewportTop, viewportHeight int) []VisibleImage {
	if viewportHeight <= 0 {
		return nil
	}
	viewportBottom := viewportTop + viewportHeight
	visible := make([]VisibleImage, 0, len(items))
	for _, item := range items {
		if item.WidthCells <= 0 || item.HeightCells <= 0 {
			continue
		}
		itemTop := item.ContentY
		itemBottom := item.ContentY + item.HeightCells
		if itemBottom <= viewportTop || itemTop >= viewportBottom {
			continue
		}
		visibleTop := maxInt(itemTop, viewportTop)
		visibleBottom := minInt(itemBottom, viewportBottom)
		height := visibleBottom - visibleTop
		if height <= 0 {
			continue
		}
		drawY := visibleTop - viewportTop
		crop := visibleImageCrop(item, visibleTop, height)
		visible = append(visible, VisibleImage{
			ID:           item.ID,
			Path:         item.Path,
			DrawY:        drawY,
			Rect:         CellRect{X: 0, Y: drawY, Width: item.WidthCells, Height: height},
			Crop:         crop,
			FullyVisible: itemTop >= viewportTop && itemBottom <= viewportBottom,
		})
	}
	return visible
}

func visibleImageCrop(item ImageLayoutItem, visibleTop, visibleHeight int) *PixelRect {
	if visibleHeight >= item.HeightCells && visibleTop <= item.ContentY {
		return nil
	}
	return &PixelRect{
		X:      0,
		Y:      maxInt(0, visibleTop-item.ContentY),
		Width:  item.WidthCells,
		Height: item.HeightCells,
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clampInt(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}
