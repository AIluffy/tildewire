package tui

import (
	"context"
	"image/color"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/zhangxueai/tildewire/internal/app"
	"github.com/zhangxueai/tildewire/internal/config"
	"github.com/zhangxueai/tildewire/internal/domain"
	tuiimage "github.com/zhangxueai/tildewire/internal/tui/image"
)

// FeedService is the application surface used by the TUI.
type FeedService interface {
	LoadFeed(context.Context, domain.SourceID, app.FeedFilter) (app.Snapshot, error)
	LoadDetail(context.Context, domain.FeedEntry) (domain.ItemDetail, error)
	ExportSaved(context.Context, app.ExportOptions) (app.ExportResult, error)
	Refresh(context.Context, domain.SourceID, app.FeedFilter, app.RefreshOptions) (app.Snapshot, error)
	ClearCache(context.Context, domain.SourceID, app.FeedFilter) (app.Snapshot, error)
	SetSaved(context.Context, string, bool, domain.SourceID, app.FeedFilter) (app.Snapshot, error)
	SetRead(context.Context, string, bool, domain.SourceID, app.FeedFilter) (app.Snapshot, error)
	SetHidden(context.Context, string, bool, domain.SourceID, app.FeedFilter) (app.Snapshot, error)
	CreatePersonalizationRule(context.Context, domain.PersonalizationRule, domain.SourceID, app.FeedFilter) (app.Snapshot, error)
	UpdatePersonalizationRule(context.Context, int64, domain.PersonalizationRule, domain.SourceID, app.FeedFilter) (app.Snapshot, error)
	SetPersonalizationRuleEnabled(context.Context, int64, bool, domain.SourceID, app.FeedFilter) (app.Snapshot, error)
	DeletePersonalizationRule(context.Context, int64, domain.SourceID, app.FeedFilter) (app.Snapshot, error)
	IgnoreDedupeCandidate(context.Context, string, domain.SourceID, app.FeedFilter) (app.Snapshot, error)
	SetSourceConfig([]domain.SourceID, map[domain.SourceID]string)
}

type inputMode int

const (
	inputModeNormal inputMode = iota
	inputModeSearch
)

type feedSelectionKey struct {
	view       domain.SourceID
	sourceView string
}

type feedSelection struct {
	cursor     int
	feedOffset int
}

// ModelOptions configure optional TUI integrations.
type ModelOptions struct {
	Config         config.Config
	FirstRun       bool
	SaveConfig     func(config.Config) error
	ImagePreviewer markdownImagePreviewer
	Images         *tuiimage.ImageManager
}

// Model is the root Bubble Tea model.
type Model struct {
	service                      FeedService
	keys                         keyMap
	help                         help.Model
	config                       config.Config
	saveConfig                   func(config.Config) error
	view                         domain.SourceID
	entries                      []domain.FeedEntry
	statuses                     []domain.SourceHealth
	fetchHistory                 []domain.FetchEvent
	rules                        []domain.PersonalizationRule
	dedupeCandidates             []domain.DedupeCandidate
	counts                       map[domain.SourceID]int
	filter                       app.FeedFilter
	mode                         inputMode
	searchDraft                  string
	searchBase                   string
	searchLoadID                 int
	paletteOpen                  bool
	paletteFilter                string
	paletteCursor                int
	paletteOffset                int
	cursor                       int
	previewOffset                int
	feedOffset                   int
	feedSelections               map[feedSelectionKey]feedSelection
	sourcesOffset                int
	activePanel                  mainPanel
	width                        int
	height                       int
	darkBackground               bool
	terminalBackground           color.Color
	refreshing                   bool
	refreshID                    int
	loadingSpinner               spinner.Model
	detail                       bool
	detailLoading                bool
	detailEntryID                string
	itemDetail                   domain.ItemDetail
	detailError                  string
	detailOffset                 int
	detailLineCache              detailLineCache
	detailImageVersion           int
	detailRawImageDrawID         int
	images                       *tuiimage.ImageManager
	imagePreviewer               markdownImagePreviewer
	imagePreviews                map[markdownImagePreviewKey]markdownImagePreviewState
	filterOpen                   bool
	filterDraft                  app.FeedFilter
	filterDraftView              domain.SourceID
	filterCursor                 int
	health                       bool
	rulesOpen                    bool
	ruleCursor                   int
	ruleForm                     *huh.Form
	ruleDraft                    ruleDraft
	ruleEditingID                int64
	dedupeOpen                   bool
	dedupeCursor                 int
	settingsOpen                 bool
	settingsCursor               int
	settingsSourceCursor         int
	settingsShowGitHubToken      bool
	settingsShowProductHuntToken bool
	settingsDraft                settingsDraft
	message                      string
	lastError                    string
	toast                        string
	toastID                      int
}

// NewModel creates the root TUI model with cached data already loaded.
func NewModel(service FeedService, initial app.Snapshot, options ...ModelOptions) Model {
	helpModel := help.New()
	helpModel.ShowAll = false
	view := initial.View
	if view == "" {
		view = domain.SourceAll
	}
	modelOptions := ModelOptions{}
	if len(options) > 0 {
		modelOptions = options[0]
	}
	if strings.TrimSpace(modelOptions.Config.GlamourStyle) == "" {
		modelOptions.Config.GlamourStyle = "dark"
	}
	if strings.TrimSpace(modelOptions.Config.MarkdownImagePreview) == "" {
		modelOptions.Config.MarkdownImagePreview = config.MarkdownImagePreviewAuto
	}
	imagePreviewer := modelOptions.ImagePreviewer
	if imagePreviewer == nil {
		imagePreviewer = newTerminalMarkdownImagePreviewer(modelOptions.Config.CacheDir)
	}
	imageManager := modelOptions.Images
	if imageManager == nil {
		imageManager = tuiimage.NewImageManager(tuiimage.ImageManagerConfig{
			PreferredProtocol:       markdownImageProtocol(modelOptions.Config.MarkdownImagePreview),
			EnableGraphicsProtocols: true,
			MaxCacheItems:           64,
			MaxDecodedBytes:         markdownImageMaxBytes * 4,
			MaxRenderConcurrency:    1,
			MaxThumbWidthCells:      96,
			MaxThumbHeightCells:     24,
		})
	}
	model := Model{
		service:          service,
		keys:             defaultKeyMap(),
		help:             helpModel,
		config:           modelOptions.Config,
		saveConfig:       modelOptions.SaveConfig,
		view:             view,
		entries:          initial.Entries,
		statuses:         initial.Statuses,
		fetchHistory:     initial.FetchHistory,
		rules:            initial.Rules,
		dedupeCandidates: initial.DedupeCandidates,
		counts:           initial.Counts,
		filter:           initial.Filter,
		feedSelections:   make(map[feedSelectionKey]feedSelection),
		width:            100,
		height:           30,
		darkBackground:   true,
		activePanel:      panelFeed,
		refreshing:       true,
		refreshID:        1,
		loadingSpinner:   spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(activeStyle)),
		images:           imageManager,
		imagePreviewer:   imagePreviewer,
		imagePreviews:    make(map[markdownImagePreviewKey]markdownImagePreviewState),
		message:          "cached feed loaded",
	}
	model.rememberFeedSelection()
	if modelOptions.FirstRun {
		model.openSettingsForm()
		model.message = "first-run settings"
	}
	return model
}

// Init starts the background refresh after cached data is visible.
func (m Model) Init() tea.Cmd {
	return m.refreshWithProgressCmd(m.refreshID, false, app.RefreshModeStartup)
}

// Update handles TUI messages and key presses.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.SetWidth(msg.Width)
		m.clampFeedOffset()
		m.clampSourcesOffset()
		m.clampPreviewOffset()
		m.clampDetailOffset()
		imageCmd := m.queueDetailImagePreviewCmds()
		if m.images != nil {
			imageCmd = batchCommands(imageCmd, m.images.OnResize(msg.Width, msg.Height))
		}
		if m.ruleForm != nil {
			m.ruleForm.WithWidth(max(40, msg.Width-4)).WithHeight(max(8, msg.Height-4))
			updated, cmd := m.ruleForm.Update(msg)
			if form, ok := updated.(*huh.Form); ok {
				m.ruleForm = form
			}
			return m, cmd
		}
		return m, batchCommands(imageCmd, m.drawDetailRawImagesCmd())
	case tuiimage.RenderedMsg:
		if m.images != nil {
			m.images.Accept(msg)
		}
		return m, nil
	case snapshotMsg:
		if msg.refreshID != 0 && msg.refreshID != m.refreshID {
			return m, nil
		}
		if msg.searchLoadID != 0 && msg.searchLoadID != m.searchLoadID {
			return m, nil
		}
		m.refreshing = false
		m.applySnapshot(msg.snapshot)
		m.lastError = ""
		m.message = msg.message
		if msg.err != nil {
			m.lastError = msg.err.Error()
			m.message = "using cached data after refresh error"
		}
		if msg.nextRefresh != nil {
			m.refreshing = true
			m.refreshID++
			if msg.err == nil {
				m.message = msg.message + "; refreshing visible scope"
			}
			return m, m.refreshWithProgressCmd(m.refreshID, msg.nextRefresh.force, msg.nextRefresh.mode)
		}
		return m, nil
	case refreshProgressMsg:
		if !m.refreshing || msg.refreshID != m.refreshID {
			return m, nil
		}
		if msg.err == nil {
			m.applySnapshot(msg.snapshot)
		}
		return m, m.refreshProgressCmd(m.refreshID)
	case spinner.TickMsg:
		if !m.refreshing && !m.detailLoading {
			return m, nil
		}
		var cmd tea.Cmd
		m.loadingSpinner, cmd = m.loadingSpinner.Update(msg)
		if m.detailLoading {
			m.clearDetailContentCache()
		}
		return m, cmd
	case detailMsg:
		if msg.itemID == m.detailEntryID {
			m.clearDetailContentCache()
			m.detailLoading = false
			m.itemDetail = msg.detail
			m.detailError = ""
			if msg.err != nil {
				m.detailError = msg.err.Error()
				m.message = "detail loaded with errors"
			} else {
				m.message = "detail loaded"
			}
			m.refreshDetailContentCache()
			m.clampDetailOffset()
			return m, batchCommands(m.queueDetailImagePreviewCmds(), m.drawDetailRawImagesCmd())
		}
		return m, nil
	case markdownImagePreviewMsg:
		if msg.itemID != m.detailEntryID {
			return m, nil
		}
		state := m.imagePreviews[msg.key]
		state.Loading = false
		if msg.err != nil {
			state.Err = msg.err.Error()
			m.message = "image preview unavailable"
		} else {
			state.Content = msg.result.Content
			state.Backend = msg.result.Backend
			state.Raw = msg.result.Raw
			state.Columns = msg.result.Columns
			state.Rows = msg.result.Rows
			m.message = "image preview loaded"
		}
		if m.imagePreviews == nil {
			m.imagePreviews = make(map[markdownImagePreviewKey]markdownImagePreviewState)
		}
		m.imagePreviews[msg.key] = state
		m.detailImageVersion++
		m.clearDetailContentCache()
		m.refreshDetailContentCache()
		m.clampDetailOffset()
		return m, m.drawDetailRawImagesCmd()
	case detailRawImageRedrawMsg:
		if msg.drawID != m.detailRawImageDrawID ||
			msg.itemID != m.detailEntryID ||
			msg.detailOffset != m.detailOffset ||
			msg.imageVersion != m.detailImageVersion {
			return m, nil
		}
		return m, m.drawDetailRawImagesCmd()
	case statusMsg:
		if msg.err != nil {
			m.lastError = msg.err.Error()
			m.message = msg.message
		} else {
			m.lastError = ""
			m.message = msg.message
		}
		if msg.toast != "" && msg.err == nil {
			m.toast = msg.toast
			m.toastID++
			return m, clearToastCmd(m.toastID)
		}
		return m, nil
	case clearToastMsg:
		if int(msg) == m.toastID {
			m.toast = ""
		}
		return m, nil
	case tea.BackgroundColorMsg:
		m.darkBackground = msg.IsDark()
		m.terminalBackground = msg.Color
		m.clearDetailContentCache()
		m.refreshDetailContentCache()
		m.clampDetailOffset()
		return m, nil
	case tea.MouseClickMsg:
		if m.settingsOpen {
			return m.handleSettingsMouseClick(msg)
		}
		if m.detail {
			return m.handleDetailMouseClick(msg)
		}
		if m.mainPanelsVisible() {
			return m.handleMouseClick(msg)
		}
	case tea.MouseWheelMsg:
		if m.detail {
			return m.handleDetailMouseWheel(msg)
		}
		if m.mainPanelsVisible() {
			return m.handleMouseWheel(msg)
		}
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	if m.ruleForm != nil {
		return m.updateRuleForm(msg)
	}
	return m, nil
}

// View renders the current screen.
func (m Model) View() tea.View {
	content := m.render()
	view := tea.NewView(content)
	view.AltScreen = true
	view.MouseMode = tea.MouseModeCellMotion
	view.WindowTitle = "tildewire"
	return view
}

func (m *Model) applySnapshot(snapshot app.Snapshot) {
	previousID := ""
	if entry, ok := m.selected(); ok {
		previousID = entry.Item.ID
	}
	previousKey := m.currentFeedSelectionKey()
	if snapshot.View != "" {
		m.view = snapshot.View
	}
	m.entries = snapshot.Entries
	m.statuses = snapshot.Statuses
	m.fetchHistory = snapshot.FetchHistory
	m.rules = snapshot.Rules
	m.dedupeCandidates = snapshot.DedupeCandidates
	m.counts = snapshot.Counts
	m.filter = snapshot.Filter
	m.clearDetailContentCache()
	m.restoreFeedSelection()
	m.clampSourcesOffset()
	if len(m.entries) == 0 {
		m.detail = false
		m.detailLoading = false
		m.detailEntryID = ""
		m.itemDetail = domain.ItemDetail{}
		m.detailError = ""
		m.detailOffset = 0
		m.resetPreviewScroll()
		m.rememberFeedSelection()
		return
	}
	if entry, ok := m.selected(); ok && m.currentFeedSelectionKey() == previousKey && entry.Item.ID == previousID {
		m.clampPreviewOffset()
	} else {
		m.resetPreviewScroll()
	}
	m.rememberFeedSelection()
	if m.detail {
		m.refreshDetailContentCache()
		m.clampDetailOffset()
	}
}

func (m Model) mainPanelsVisible() bool {
	return !m.settingsOpen && m.ruleForm == nil && !m.filterOpen && !m.health && !m.rulesOpen && !m.dedupeOpen && !m.detail && !m.paletteOpen
}
