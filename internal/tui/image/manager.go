package image

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
)

const (
	defaultRenderTimeout        = 12 * time.Second
	protocolFailureDisableAfter = 2
	defaultTerminalCellWidth    = 8
	defaultTerminalCellHeight   = 16
)

// ImageManager owns image protocol selection, async rendering, cache, and fallback state.
type ImageManager struct {
	mu sync.Mutex

	config ImageManagerConfig
	cache  *ImageCache

	backend imageBackend
	probe   terminalProbe
	metrics TerminalMetrics
	sem     chan struct{}

	currentVersion          uint64
	protocolFailures        int
	sessionGraphicsDisabled bool
}

type managerOption func(*ImageManager)

// NewImageManager creates a terminal image manager.
func NewImageManager(config ImageManagerConfig) *ImageManager {
	return newImageManager(config)
}

func newImageManager(config ImageManagerConfig, options ...managerOption) *ImageManager {
	config = config.normalized()
	manager := &ImageManager{
		config:  config,
		cache:   NewImageCache(config.MaxCacheItems, config.MaxDecodedBytes),
		backend: termimgBackend{},
		probe:   realTerminalProbe{},
		metrics: TerminalMetrics{FontWidth: defaultTerminalCellWidth, FontHeight: defaultTerminalCellHeight},
		sem:     make(chan struct{}, config.MaxRenderConcurrency),
	}
	for _, option := range options {
		option(manager)
	}
	if manager.cache == nil {
		manager.cache = NewImageCache(config.MaxCacheItems, config.MaxDecodedBytes)
	}
	if manager.backend == nil {
		manager.backend = termimgBackend{}
	}
	if manager.probe == nil {
		manager.probe = realTerminalProbe{}
	}
	manager.refreshMetricsLocked(0, 0)
	return manager
}

func withImageBackend(backend imageBackend) managerOption {
	return func(manager *ImageManager) {
		manager.backend = backend
	}
}

func withTerminalProbe(probe terminalProbe) managerOption {
	return func(manager *ImageManager) {
		manager.probe = probe
	}
}

// CurrentVersion returns the latest image render version.
func (m *ImageManager) CurrentVersion() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.currentVersion
}

// ProtocolFor returns the current stable protocol for a render mode.
func (m *ImageManager) ProtocolFor(mode ImageRenderMode) ImageProtocol {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.protocolForLocked(mode)
}

// OnResize updates terminal dimensions, invalidates rendered strings, and bumps the render version.
func (m *ImageManager) OnResize(width, height int) tea.Cmd {
	m.mu.Lock()
	previous := m.metrics
	m.refreshMetricsLocked(width, height)
	if previous != m.metrics {
		m.cache.InvalidateRendered()
		m.currentVersion++
	}
	m.mu.Unlock()
	return nil
}

// Prefetch starts asynchronous rendering for a batch of image requests.
func (m *ImageManager) Prefetch(requests []ImageRequest) tea.Cmd {
	if len(requests) == 0 {
		return nil
	}
	version := m.bumpVersion()
	cmds := make([]tea.Cmd, 0, len(requests))
	for _, request := range requests {
		request = m.normalizeRequest(request)
		if request.Rect.Width <= 0 || request.Rect.Height <= 0 {
			continue
		}
		cmds = append(cmds, m.renderJobCmd(RenderJob{Request: request, Version: version}))
	}
	if len(cmds) == 0 {
		return nil
	}
	if len(cmds) == 1 {
		return cmds[0]
	}
	return tea.Batch(cmds...)
}

// RenderImageAsync starts asynchronous rendering for one image request.
func (m *ImageManager) RenderImageAsync(request ImageRequest) tea.Cmd {
	return m.Prefetch([]ImageRequest{request})
}

// RenderNow renders one image using the manager cache and fallback policy.
func (m *ImageManager) RenderNow(ctx context.Context, request ImageRequest) RenderedMsg {
	return m.renderJob(ctx, RenderJob{Request: m.normalizeRequest(request), Version: m.CurrentVersion()})
}

// Accept stores a render result unless it is stale.
func (m *ImageManager) Accept(msg RenderedMsg) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if msg.Version < m.currentVersion {
		return false
	}
	if msg.Version > m.currentVersion {
		m.currentVersion = msg.Version
	}
	if msg.FallbackFrom != "" && msg.FallbackFrom != ProtocolHalfblocks {
		m.recordProtocolFailureLocked()
	}
	if msg.Err != nil && msg.Err.Kind == ErrProtocolFailed && msg.Err.Protocol != ProtocolHalfblocks {
		m.recordProtocolFailureLocked()
	}
	if msg.Image.Cells == "" {
		return true
	}
	if err := m.cache.PutRendered(m.normalizeRequest(msg.Request), msg.Image.Protocol, m.metrics, msg.Image); err != nil {
		return true
	}
	return true
}

// Rendered returns a cached image or a stable placeholder. It never performs heavy work.
func (m *ImageManager) Rendered(request ImageRequest) RenderedImage {
	request = m.normalizeRequest(request)
	m.mu.Lock()
	protocol := m.protocolForLocked(request.Mode)
	metrics := m.metrics
	cache := m.cache
	m.mu.Unlock()
	if imageValue, ok := cache.GetRendered(request, protocol, metrics); ok {
		return imageValue
	}
	if protocol != ProtocolHalfblocks {
		if imageValue, ok := cache.GetRendered(request, ProtocolHalfblocks, metrics); ok {
			return imageValue
		}
	}
	return Placeholder(request)
}

func (m *ImageManager) renderJobCmd(job RenderJob) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), defaultRenderTimeout)
		defer cancel()
		return m.renderJob(ctx, job)
	}
}

func (m *ImageManager) renderJob(ctx context.Context, job RenderJob) RenderedMsg {
	request := m.normalizeRequest(job.Request)
	if request.Rect.Width <= 0 || request.Rect.Height <= 0 {
		return RenderedMsg{
			Version: job.Version,
			Request: request,
			Image:   Placeholder(request),
			Err:     renderError(ErrResizeFailed, ProtocolHalfblocks, request.Path, errors.New("image rect must be positive")),
		}
	}
	if m.shouldDebounce(request) {
		timer := time.NewTimer(m.config.ScrollRenderDebounce)
		select {
		case <-ctx.Done():
			timer.Stop()
			return m.timeoutMsg(job, request)
		case <-timer.C:
		}
	}
	if err := m.acquire(ctx); err != nil {
		return m.timeoutMsg(job, request)
	}
	defer m.release()

	m.mu.Lock()
	protocol := m.protocolForLocked(request.Mode)
	metrics := m.metrics
	cache := m.cache
	backend := m.backend
	probe := m.probe
	m.mu.Unlock()

	if textOnlyTerminal(probe) {
		return RenderedMsg{
			Version: job.Version,
			Request: request,
			Image:   Placeholder(request),
			Err:     renderError(ErrUnsupportedTerminal, ProtocolHalfblocks, request.Path, errors.New("terminal cannot safely render image cells")),
		}
	}
	if cached, ok := cache.GetRendered(request, protocol, metrics); ok {
		return RenderedMsg{Version: job.Version, Request: request, Image: cached}
	}

	source, _, err := cache.Decode(ctx, request.Path)
	if err != nil {
		return RenderedMsg{
			Version: job.Version,
			Request: request,
			Image:   Placeholder(request),
			Err:     renderError(ErrDecodeFailed, ProtocolHalfblocks, request.Path, err),
		}
	}

	cells, err := backend.Render(ctx, source, request, protocol, metrics)
	if err == nil {
		return RenderedMsg{
			Version: job.Version,
			Request: request,
			Image:   renderedImageFromCells(request, protocol, cells),
		}
	}
	if protocol != ProtocolHalfblocks {
		cells, fallbackErr := backend.Render(ctx, source, request, ProtocolHalfblocks, metrics)
		if fallbackErr == nil {
			return RenderedMsg{
				Version:      job.Version,
				Request:      request,
				Image:        renderedImageFromCells(request, ProtocolHalfblocks, cells),
				FallbackFrom: protocol,
			}
		}
		return RenderedMsg{
			Version:      job.Version,
			Request:      request,
			Image:        Placeholder(request),
			Err:          renderError(ErrProtocolFailed, protocol, request.Path, errors.Join(err, fallbackErr)),
			FallbackFrom: protocol,
		}
	}
	return RenderedMsg{
		Version: job.Version,
		Request: request,
		Image:   Placeholder(request),
		Err:     renderError(ErrProtocolFailed, protocol, request.Path, err),
	}
}

func (m *ImageManager) protocolForLocked(mode ImageRenderMode) ImageProtocol {
	return detectStableProtocol(m.config, normalizeRenderMode(mode), m.probe, m.sessionGraphicsDisabled)
}

func (m *ImageManager) recordProtocolFailureLocked() {
	m.protocolFailures++
	inTmux := strings.TrimSpace(m.probe.Env("TMUX")) != "" || strings.EqualFold(m.probe.Env("TERM_PROGRAM"), "tmux")
	if inTmux || m.protocolFailures >= protocolFailureDisableAfter {
		m.sessionGraphicsDisabled = true
		m.cache.InvalidateRendered()
		m.currentVersion++
	}
}

func (m *ImageManager) bumpVersion() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentVersion++
	return m.currentVersion
}

func (m *ImageManager) normalizeRequest(request ImageRequest) ImageRequest {
	config := m.config
	request.Mode = normalizeRenderMode(request.Mode)
	request.Rect.Width = clampInt(request.Rect.Width, 0, config.MaxThumbWidthCells)
	request.Rect.Height = clampInt(request.Rect.Height, 0, config.MaxThumbHeightCells)
	return request
}

func (m *ImageManager) refreshMetricsLocked(width, height int) {
	metrics := m.probe.Metrics()
	if metrics.FontWidth <= 0 {
		metrics.FontWidth = defaultTerminalCellWidth
	}
	if metrics.FontHeight <= 0 {
		metrics.FontHeight = defaultTerminalCellHeight
	}
	if width > 0 {
		metrics.Columns = width
	}
	if height > 0 {
		metrics.Rows = height
	}
	m.metrics = metrics
}

func (m *ImageManager) shouldDebounce(request ImageRequest) bool {
	return request.Mode == RenderModeScrollableInline && m.config.ScrollRenderDebounce > 0
}

func (m *ImageManager) acquire(ctx context.Context) error {
	select {
	case m.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *ImageManager) release() {
	<-m.sem
}

func (m *ImageManager) timeoutMsg(job RenderJob, request ImageRequest) RenderedMsg {
	return RenderedMsg{
		Version: job.Version,
		Request: request,
		Image:   Placeholder(request),
		Err:     renderError(ErrTimeout, ProtocolHalfblocks, request.Path, context.DeadlineExceeded),
	}
}

func textOnlyTerminal(probe terminalProbe) bool {
	if !probe.IsStdoutTTY() || !probe.LocaleIsUTF8() {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(probe.Env("TERM")), "dumb") || truthy(probe.Env("CI")) {
		return true
	}
	return false
}

func renderedImageFromCells(request ImageRequest, protocol ImageProtocol, cells string) RenderedImage {
	return RenderedImage{
		ID:          request.ID,
		Protocol:    protocol,
		Cells:       cells,
		Rect:        request.Rect,
		WidthCells:  request.Rect.Width,
		HeightCells: request.Rect.Height,
	}
}
