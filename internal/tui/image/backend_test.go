package image

import (
	stdimage "image"
	"image/color"
	"testing"
)

func TestCropSourceForRequestUsesVisibleRowsFromFullImageSlot(t *testing.T) {
	source := gradientImage(10, 10)
	request := ImageRequest{
		Rect: CellRect{Width: 10, Height: 3},
		Crop: &PixelRect{X: 0, Y: 2, Width: 10, Height: 5},
	}

	cropped, err := cropSourceForRequest(source, request)
	if err != nil {
		t.Fatal(err)
	}
	bounds := cropped.Bounds()
	if bounds.Dx() != 10 || bounds.Dy() != 6 {
		t.Fatalf("cropped bounds = %v, want 10x6", bounds)
	}
	if got := color.RGBAModel.Convert(cropped.At(bounds.Min.X, bounds.Min.Y)).(color.RGBA).G; got != 4 {
		t.Fatalf("cropped top row came from source y=%d, want 4", got)
	}
}

func TestCropSourceForRequestUsesTopRowsForBottomClippedSlot(t *testing.T) {
	source := gradientImage(10, 10)
	request := ImageRequest{
		Rect: CellRect{Width: 10, Height: 2},
		Crop: &PixelRect{X: 0, Y: 0, Width: 10, Height: 5},
	}

	cropped, err := cropSourceForRequest(source, request)
	if err != nil {
		t.Fatal(err)
	}
	bounds := cropped.Bounds()
	if bounds.Dx() != 10 || bounds.Dy() != 4 {
		t.Fatalf("cropped bounds = %v, want 10x4", bounds)
	}
	if got := color.RGBAModel.Convert(cropped.At(bounds.Min.X, bounds.Max.Y-1)).(color.RGBA).G; got != 3 {
		t.Fatalf("cropped bottom row came from source y=%d, want 3", got)
	}
}

func gradientImage(width, height int) stdimage.Image {
	img := stdimage.NewRGBA(stdimage.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 0x88, A: 0xff})
		}
	}
	return img
}
