package image

import (
	"container/list"
	"context"
	stdimage "image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"strconv"
	"sync"
)

// ImageCache stores decoded images and rendered terminal cell strings.
type ImageCache struct {
	mu sync.Mutex

	maxItems        int
	maxDecodedBytes int64

	decoded      map[decodedCacheKey]*decodedCacheEntry
	decodedList  *list.List
	decodedBytes int64

	rendered     map[renderedCacheKey]*renderedCacheEntry
	renderedList *list.List
}

type fileSignature struct {
	Path      string
	Size      int64
	ModTimeNS int64
	Exists    bool
}

type decodedCacheKey struct {
	Path      string
	Size      int64
	ModTimeNS int64
}

type decodedCacheEntry struct {
	key   decodedCacheKey
	image stdimage.Image
	bytes int64
	elem  *list.Element
}

type renderedCacheKey struct {
	ID          string
	Path        string
	Size        int64
	ModTimeNS   int64
	Protocol    ImageProtocol
	WidthCells  int
	HeightCells int
	Crop        string
	FontWidth   int
	FontHeight  int
	ScaleMode   string
	DitherMode  string
	ColorMode   string
}

type renderedCacheEntry struct {
	key   renderedCacheKey
	image RenderedImage
	elem  *list.Element
}

// NewImageCache creates an image cache with LRU eviction.
func NewImageCache(maxItems int, maxDecodedBytes int64) *ImageCache {
	if maxItems <= 0 {
		maxItems = 64
	}
	if maxDecodedBytes <= 0 {
		maxDecodedBytes = 64 << 20
	}
	return &ImageCache{
		maxItems:        maxItems,
		maxDecodedBytes: maxDecodedBytes,
		decoded:         make(map[decodedCacheKey]*decodedCacheEntry),
		decodedList:     list.New(),
		rendered:        make(map[renderedCacheKey]*renderedCacheEntry),
		renderedList:    list.New(),
	}
}

// Decode returns a decoded image from cache or decodes it from disk.
func (c *ImageCache) Decode(ctx context.Context, path string) (stdimage.Image, fileSignature, error) {
	if err := ctx.Err(); err != nil {
		return nil, fileSignature{}, err
	}
	signature, err := fileSignatureForPath(path)
	if err != nil {
		return nil, fileSignature{}, err
	}
	key := decodedCacheKey{Path: signature.Path, Size: signature.Size, ModTimeNS: signature.ModTimeNS}

	c.mu.Lock()
	if entry, ok := c.decoded[key]; ok {
		c.decodedList.MoveToFront(entry.elem)
		imageValue := entry.image
		c.mu.Unlock()
		return imageValue, signature, nil
	}
	c.mu.Unlock()

	file, err := os.Open(path)
	if err != nil {
		return nil, signature, err
	}
	defer file.Close() //nolint:errcheck
	imageValue, _, err := stdimage.Decode(file)
	if err != nil {
		return nil, signature, err
	}
	if err := ctx.Err(); err != nil {
		return nil, signature, err
	}
	bytes := decodedImageBytes(imageValue)

	c.mu.Lock()
	if bytes <= c.maxDecodedBytes {
		entry := &decodedCacheEntry{key: key, image: imageValue, bytes: bytes}
		entry.elem = c.decodedList.PushFront(entry)
		c.decoded[key] = entry
		c.decodedBytes += bytes
		c.evictDecodedLocked()
	}
	c.mu.Unlock()

	return imageValue, signature, nil
}

// GetRendered looks up a rendered image for the current file signature and terminal metrics.
func (c *ImageCache) GetRendered(request ImageRequest, protocol ImageProtocol, metrics TerminalMetrics) (RenderedImage, bool) {
	signature, err := fileSignatureForPath(request.Path)
	if err != nil {
		return RenderedImage{}, false
	}
	key := renderedKey(request, protocol, metrics, signature)
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.rendered[key]
	if !ok {
		return RenderedImage{}, false
	}
	c.renderedList.MoveToFront(entry.elem)
	imageValue := entry.image
	imageValue.FromCache = true
	return imageValue, true
}

// PutRendered stores a rendered image for the current file signature and terminal metrics.
func (c *ImageCache) PutRendered(request ImageRequest, protocol ImageProtocol, metrics TerminalMetrics, imageValue RenderedImage) error {
	signature, err := fileSignatureForPath(request.Path)
	if err != nil {
		return err
	}
	key := renderedKey(request, protocol, metrics, signature)
	c.mu.Lock()
	defer c.mu.Unlock()
	if entry, ok := c.rendered[key]; ok {
		entry.image = imageValue
		c.renderedList.MoveToFront(entry.elem)
		return nil
	}
	entry := &renderedCacheEntry{key: key, image: imageValue}
	entry.elem = c.renderedList.PushFront(entry)
	c.rendered[key] = entry
	c.evictRenderedLocked()
	return nil
}

// InvalidateRendered clears cached terminal strings while preserving decoded images.
func (c *ImageCache) InvalidateRendered() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rendered = make(map[renderedCacheKey]*renderedCacheEntry)
	c.renderedList.Init()
}

func (c *ImageCache) evictDecodedLocked() {
	for c.decodedList.Len() > c.maxItems || c.decodedBytes > c.maxDecodedBytes {
		elem := c.decodedList.Back()
		if elem == nil {
			return
		}
		entry := elem.Value.(*decodedCacheEntry)
		delete(c.decoded, entry.key)
		c.decodedBytes -= entry.bytes
		c.decodedList.Remove(elem)
	}
}

func (c *ImageCache) evictRenderedLocked() {
	for c.renderedList.Len() > c.maxItems {
		elem := c.renderedList.Back()
		if elem == nil {
			return
		}
		entry := elem.Value.(*renderedCacheEntry)
		delete(c.rendered, entry.key)
		c.renderedList.Remove(elem)
	}
}

func fileSignatureForPath(path string) (fileSignature, error) {
	info, err := os.Stat(path)
	if err != nil {
		return fileSignature{}, err
	}
	return fileSignature{
		Path:      path,
		Size:      info.Size(),
		ModTimeNS: info.ModTime().UnixNano(),
		Exists:    true,
	}, nil
}

func renderedKey(request ImageRequest, protocol ImageProtocol, metrics TerminalMetrics, signature fileSignature) renderedCacheKey {
	return renderedCacheKey{
		ID:          request.ID,
		Path:        signature.Path,
		Size:        signature.Size,
		ModTimeNS:   signature.ModTimeNS,
		Protocol:    normalizeProtocol(protocol),
		WidthCells:  request.Rect.Width,
		HeightCells: request.Rect.Height,
		Crop:        cropKey(request.Crop),
		FontWidth:   metrics.FontWidth,
		FontHeight:  metrics.FontHeight,
		ScaleMode:   "fit",
		DitherMode:  "none",
		ColorMode:   "auto",
	}
}

func cropKey(crop *PixelRect) string {
	if crop == nil {
		return ""
	}
	return strconv.Itoa(crop.X) + "," + strconv.Itoa(crop.Y) + "," + strconv.Itoa(crop.Width) + "," + strconv.Itoa(crop.Height)
}

func decodedImageBytes(imageValue stdimage.Image) int64 {
	if imageValue == nil {
		return 0
	}
	bounds := imageValue.Bounds()
	return int64(maxInt(1, bounds.Dx())) * int64(maxInt(1, bounds.Dy())) * 4
}
