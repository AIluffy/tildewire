package markdown

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/AIluffy/tildewire/internal/config"
	tuiimage "github.com/AIluffy/tildewire/internal/tui/image"
)

// MaxImageBytes caps downloaded Markdown image data.
const MaxImageBytes = 8 << 20

const markdownImageTimeout = 12 * time.Second

const (
	markdownImagePreviewMaxWidthCells  = 96
	markdownImagePreviewMaxHeightCells = 40
)

// ImageRequest describes one Markdown image render request.
type ImageRequest struct {
	URL       string
	Alt       string
	Mode      string
	CacheDir  string
	Width     int
	MaxHeight int
}

// ImageResult is a rendered terminal image block.
type ImageResult struct {
	Content string
	Backend string
	Raw     bool
	Columns int
	Rows    int
}

// Previewer renders Markdown image previews.
type Previewer interface {
	RenderMarkdownImage(context.Context, ImageRequest) (ImageResult, error)
}

// TerminalImagePreviewer renders Markdown images using terminal graphics or halfblocks.
type TerminalImagePreviewer struct {
	mu       sync.Mutex
	cacheDir string
	managers map[string]*tuiimage.ImageManager
}

// NewTerminalImagePreviewer creates a terminal image previewer backed by cacheDir.
func NewTerminalImagePreviewer(cacheDir string) *TerminalImagePreviewer {
	return &TerminalImagePreviewer{
		cacheDir: cacheDir,
		managers: make(map[string]*tuiimage.ImageManager),
	}
}

// RenderMarkdownImage renders a Markdown image to terminal cells.
func (p *TerminalImagePreviewer) RenderMarkdownImage(ctx context.Context, request ImageRequest) (ImageResult, error) {
	mode := NormalizeImagePreviewMode(request.Mode)
	if mode == config.MarkdownImagePreviewOff {
		return ImageResult{}, fmt.Errorf("markdown image preview disabled")
	}
	if strings.TrimSpace(request.CacheDir) == "" {
		request.CacheDir = p.cacheDir
	}
	imagePath, err := cacheMarkdownImage(ctx, request.URL, request.CacheDir)
	if err != nil {
		return ImageResult{}, err
	}
	width := clamp(request.Width, 16, markdownImagePreviewMaxWidthCells)
	height := clamp(request.MaxHeight, 4, markdownImagePreviewMaxHeightCells)
	manager := p.managerForMode(mode)
	msg := manager.RenderNow(ctx, tuiimage.ImageRequest{
		ID:      request.URL,
		Path:    imagePath,
		Mode:    ImageRenderMode(mode),
		Rect:    tuiimage.CellRect{Width: width, Height: height},
		AltText: request.Alt,
	})
	manager.Accept(msg)
	if strings.TrimSpace(msg.Image.Cells) == "" && msg.Err != nil {
		return ImageResult{}, msg.Err
	}
	return ImageResult{
		Content: strings.TrimRight(msg.Image.Cells, "\n"),
		Backend: string(msg.Image.Protocol),
		Raw:     msg.Image.Protocol != tuiimage.ProtocolHalfblocks,
		Columns: msg.Image.WidthCells,
		Rows:    msg.Image.HeightCells,
	}, nil
}

func (p *TerminalImagePreviewer) managerForMode(mode string) *tuiimage.ImageManager {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.managers == nil {
		p.managers = make(map[string]*tuiimage.ImageManager)
	}
	mode = NormalizeImagePreviewMode(mode)
	if manager := p.managers[mode]; manager != nil {
		return manager
	}
	manager := tuiimage.NewImageManager(tuiimage.ImageManagerConfig{
		PreferredProtocol:       ImageProtocol(mode),
		EnableGraphicsProtocols: mode != config.MarkdownImagePreviewHalfblocks,
		MaxCacheItems:           64,
		MaxDecodedBytes:         MaxImageBytes * 4,
		MaxRenderConcurrency:    1,
		MaxThumbWidthCells:      markdownImagePreviewMaxWidthCells,
		MaxThumbHeightCells:     markdownImagePreviewMaxHeightCells,
	})
	p.managers[mode] = manager
	return manager
}

// ImageRenderMode returns the image manager render mode for a Markdown image preview mode.
func ImageRenderMode(mode string) tuiimage.ImageRenderMode {
	switch NormalizeImagePreviewMode(mode) {
	case config.MarkdownImagePreviewHalfblocks:
		return tuiimage.RenderModeScrollableInline
	default:
		return tuiimage.RenderModePreviewPane
	}
}

// ImageProtocol returns the preferred terminal image protocol for a Markdown image preview mode.
func ImageProtocol(mode string) tuiimage.ImageProtocol {
	switch NormalizeImagePreviewMode(mode) {
	case config.MarkdownImagePreviewKitty:
		return tuiimage.ProtocolKitty
	case config.MarkdownImagePreviewITerm:
		return tuiimage.ProtocolITerm2
	case config.MarkdownImagePreviewSixel:
		return tuiimage.ProtocolSixel
	case config.MarkdownImagePreviewHalfblocks:
		return tuiimage.ProtocolHalfblocks
	default:
		return tuiimage.ProtocolAuto
	}
}

// NormalizeImagePreviewMode normalizes config and legacy Markdown image preview values.
func NormalizeImagePreviewMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case config.MarkdownImagePreviewOff:
		return config.MarkdownImagePreviewOff
	case config.MarkdownImagePreviewKitty:
		return config.MarkdownImagePreviewKitty
	case config.MarkdownImagePreviewITerm:
		return config.MarkdownImagePreviewITerm
	case config.MarkdownImagePreviewSixel:
		return config.MarkdownImagePreviewSixel
	case config.MarkdownImagePreviewHalfblocks, "chafa":
		return config.MarkdownImagePreviewHalfblocks
	default:
		return config.MarkdownImagePreviewAuto
	}
}

// ScrollableImagePreviewMode resolves auto mode to a stable scrollable renderer.
func ScrollableImagePreviewMode(mode string) string {
	mode = NormalizeImagePreviewMode(mode)
	if mode == config.MarkdownImagePreviewAuto {
		return config.MarkdownImagePreviewHalfblocks
	}
	return mode
}

func cacheMarkdownImage(ctx context.Context, rawURL, cacheDir string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid image URL %q", rawURL)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("unsupported image URL scheme %q", parsed.Scheme)
	}
	dir, err := markdownImageCacheDir(cacheDir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(rawURL))
	imagePath := filepath.Join(dir, hex.EncodeToString(sum[:])+imageCacheExtension(parsed.Path))
	if info, err := os.Stat(imagePath); err == nil && info.Size() > 0 {
		return imagePath, nil
	}

	requestCtx, cancel := context.WithTimeout(ctx, markdownImageTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "tildewire/0.1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("http %d for image %s", resp.StatusCode, rawURL)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxImageBytes+1))
	if err != nil {
		return "", err
	}
	if len(data) > MaxImageBytes {
		return "", fmt.Errorf("image exceeds %d bytes", MaxImageBytes)
	}
	tmp := imagePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, imagePath); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return imagePath, nil
}

func markdownImageCacheDir(cacheDir string) (string, error) {
	if strings.TrimSpace(cacheDir) != "" {
		return filepath.Join(cacheDir, "images"), nil
	}
	userCache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(userCache, "tildewire", "images"), nil
}

func imageCacheExtension(rawPath string) string {
	ext := strings.ToLower(path.Ext(rawPath))
	switch ext {
	case ".gif", ".jpg", ".jpeg", ".png", ".svg", ".webp":
		return ext
	default:
		return ".img"
	}
}

func clamp(value, low, high int) int {
	return min(max(value, low), high)
}
