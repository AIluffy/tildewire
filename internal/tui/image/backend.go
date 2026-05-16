package image

import (
	"context"
	"fmt"
	stdimage "image"
	"image/draw"

	"github.com/blacktop/go-termimg"
)

type imageBackend interface {
	Render(context.Context, stdimage.Image, ImageRequest, ImageProtocol, TerminalMetrics) (string, error)
}

type termimgBackend struct{}

func (termimgBackend) Render(ctx context.Context, source stdimage.Image, request ImageRequest, protocol ImageProtocol, _ TerminalMetrics) (string, error) {
	if source == nil {
		return "", fmt.Errorf("image source is nil")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	source, err := cropSourceForRequest(source, request)
	if err != nil {
		return "", err
	}

	width := maxInt(1, request.Rect.Width)
	height := maxInt(1, request.Rect.Height)
	result := make(chan renderResult, 1)
	go func() {
		img := termimg.New(source).
			Size(width, height).
			Scale(termimg.ScaleFit).
			Protocol(toTermimgProtocol(protocol)).
			Dither(ditherForProtocol(protocol))
		if normalizeProtocol(protocol) == ProtocolKitty {
			img = img.Virtual(false).UseUnicode(false)
		}
		cells, err := img.Render()
		result <- renderResult{cells: cells, err: err}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case rendered := <-result:
		if rendered.err != nil {
			return "", rendered.err
		}
		if rendered.cells == "" {
			return "", fmt.Errorf("empty render result")
		}
		return rendered.cells, nil
	}
}

func cropSourceForRequest(source stdimage.Image, request ImageRequest) (stdimage.Image, error) {
	if source == nil || request.Crop == nil {
		return source, nil
	}
	crop := request.Crop
	if crop.Width <= 0 || crop.Height <= 0 || request.Rect.Width <= 0 || request.Rect.Height <= 0 {
		return source, nil
	}
	bounds := source.Bounds()
	sourceWidth := bounds.Dx()
	sourceHeight := bounds.Dy()
	if sourceWidth <= 0 || sourceHeight <= 0 {
		return source, nil
	}
	cellX0 := clampInt(crop.X, 0, crop.Width)
	cellY0 := clampInt(crop.Y, 0, crop.Height)
	cellX1 := clampInt(crop.X+request.Rect.Width, cellX0+1, crop.Width)
	cellY1 := clampInt(crop.Y+request.Rect.Height, cellY0+1, crop.Height)
	pixelRect := stdimage.Rect(
		bounds.Min.X+cellX0*sourceWidth/crop.Width,
		bounds.Min.Y+cellY0*sourceHeight/crop.Height,
		bounds.Min.X+cellX1*sourceWidth/crop.Width,
		bounds.Min.Y+cellY1*sourceHeight/crop.Height,
	).Intersect(bounds)
	if pixelRect.Empty() {
		return nil, fmt.Errorf("crop rectangle is empty")
	}
	if subImage, ok := source.(interface {
		SubImage(stdimage.Rectangle) stdimage.Image
	}); ok {
		return subImage.SubImage(pixelRect), nil
	}
	target := stdimage.NewRGBA(stdimage.Rect(0, 0, pixelRect.Dx(), pixelRect.Dy()))
	draw.Draw(target, target.Bounds(), source, pixelRect.Min, draw.Src)
	return target, nil
}

func ditherForProtocol(protocol ImageProtocol) bool {
	return normalizeProtocol(protocol) == ProtocolHalfblocks
}

type renderResult struct {
	cells string
	err   error
}
