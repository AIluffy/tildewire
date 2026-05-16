package image

import (
	"fmt"
	"time"
)

// ImageProtocol names a terminal image rendering protocol.
type ImageProtocol string

const (
	// ProtocolAuto asks the manager to choose the most stable available protocol.
	ProtocolAuto ImageProtocol = "auto"
	// ProtocolKitty uses the Kitty graphics protocol when it is safe.
	ProtocolKitty ImageProtocol = "kitty"
	// ProtocolITerm2 uses the iTerm2 inline image protocol when it is safe.
	ProtocolITerm2 ImageProtocol = "iterm2"
	// ProtocolSixel uses Sixel when it is safe.
	ProtocolSixel ImageProtocol = "sixel"
	// ProtocolHalfblocks renders images as Unicode halfblocks.
	ProtocolHalfblocks ImageProtocol = "halfblocks"
)

// ImageRenderMode describes where an image will be drawn in the TUI.
type ImageRenderMode string

const (
	// RenderModePreviewPane is a fixed preview pane. Graphics protocols may be used here.
	RenderModePreviewPane ImageRenderMode = "preview-pane"
	// RenderModeScrollableInline is an image embedded in scrollable content.
	RenderModeScrollableInline ImageRenderMode = "scrollable-inline"
)

// ImageManagerConfig controls protocol selection, caching, and render throttling.
type ImageManagerConfig struct {
	PreferredProtocol ImageProtocol

	EnableGraphicsProtocols bool
	EnableTmuxPassthrough   bool

	// UnsafeInlineGraphicsInScrollableList allows Kitty/iTerm2/Sixel in scrollable lists.
	// The default is false because protocol images can leave stale placements while scrolling.
	UnsafeInlineGraphicsInScrollableList bool

	MaxCacheItems        int
	MaxDecodedBytes      int64
	MaxRenderConcurrency int

	// ScrollRenderDebounce delays render work for scrollable inline images during fast scrolling.
	ScrollRenderDebounce time.Duration

	MaxThumbWidthCells  int
	MaxThumbHeightCells int
}

// CellRect is a terminal-cell rectangle.
type CellRect struct {
	X      int
	Y      int
	Width  int
	Height int
}

// PixelRect is a source-slot rectangle used for proportional crop requests.
// X and Y are the visible cell offset; Width and Height are the full slot size.
type PixelRect struct {
	X      int
	Y      int
	Width  int
	Height int
}

// ImageRequest describes one logical image render.
type ImageRequest struct {
	ID      string
	Path    string
	Mode    ImageRenderMode
	Rect    CellRect
	Crop    *PixelRect
	AltText string
}

// RenderedImage is a cached or freshly rendered terminal image block.
type RenderedImage struct {
	ID          string
	Protocol    ImageProtocol
	Cells       string
	Rect        CellRect
	WidthCells  int
	HeightCells int
	FromCache   bool
}

// RenderJob ties an image request to the TUI version that requested it.
type RenderJob struct {
	Request ImageRequest
	Version uint64
}

// RenderedMsg is emitted by ImageManager render commands.
type RenderedMsg struct {
	Version      uint64
	Request      ImageRequest
	Image        RenderedImage
	Err          *RenderError
	FallbackFrom ImageProtocol
}

// TerminalMetrics captures terminal cell and window dimensions used in cache keys.
type TerminalMetrics struct {
	FontWidth  int
	FontHeight int
	Columns    int
	Rows       int
}

// RenderErrorKind classifies image render failures without exposing stack details to the UI.
type RenderErrorKind string

const (
	// ErrUnsupportedTerminal means the terminal cannot safely render image cells.
	ErrUnsupportedTerminal RenderErrorKind = "unsupported-terminal"
	// ErrDecodeFailed means the image file could not be decoded.
	ErrDecodeFailed RenderErrorKind = "decode-failed"
	// ErrResizeFailed means sizing or crop preparation failed.
	ErrResizeFailed RenderErrorKind = "resize-failed"
	// ErrProtocolFailed means a terminal image protocol failed.
	ErrProtocolFailed RenderErrorKind = "protocol-failed"
	// ErrTimeout means rendering exceeded its context deadline.
	ErrTimeout RenderErrorKind = "timeout"
)

// RenderError is a UI-safe image error with the original cause retained for debug logging.
type RenderError struct {
	Kind     RenderErrorKind
	Protocol ImageProtocol
	Path     string
	Message  string
	Cause    error
}

// Error returns a concise error string suitable for logs.
func (e *RenderError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return fmt.Sprintf("%s: %s", e.Kind, e.Message)
	}
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Kind, e.Cause)
	}
	return string(e.Kind)
}

func renderError(kind RenderErrorKind, protocol ImageProtocol, path string, err error) *RenderError {
	message := ""
	if err != nil {
		message = err.Error()
	}
	return &RenderError{Kind: kind, Protocol: protocol, Path: path, Message: message, Cause: err}
}

func (c ImageManagerConfig) normalized() ImageManagerConfig {
	if c.PreferredProtocol == "" {
		c.PreferredProtocol = ProtocolAuto
	}
	if c.MaxCacheItems <= 0 {
		c.MaxCacheItems = 64
	}
	if c.MaxDecodedBytes <= 0 {
		c.MaxDecodedBytes = 64 << 20
	}
	if c.MaxRenderConcurrency <= 0 {
		c.MaxRenderConcurrency = 1
	}
	if c.MaxThumbWidthCells <= 0 {
		c.MaxThumbWidthCells = 64
	}
	if c.MaxThumbHeightCells <= 0 {
		c.MaxThumbHeightCells = 18
	}
	return c
}
