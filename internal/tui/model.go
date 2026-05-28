package tui

import (
	"image/color"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/AIluffy/tildewire/internal/app"
	"github.com/AIluffy/tildewire/internal/config"
	"github.com/AIluffy/tildewire/internal/domain"
	tuiimage "github.com/AIluffy/tildewire/internal/tui/image"
)

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
	service    FeedService
	keys       keyMap
	help       help.Model
	config     config.Config
	saveConfig func(config.Config) error
	feedState
	refreshState
	detailState
	overlayState
	settingsState
	styles             themeStyles
	width              int
	height             int
	darkBackground     bool
	terminalBackground color.Color
}

// NewModel creates the root TUI model with cached data already loaded.
func NewModel(service FeedService, initial app.Snapshot, options ...ModelOptions) Model {
	helpModel := help.New()
	helpModel.ShowAll = false
	view := initial.View
	if view == "" {
		view = app.DefaultStartupView
	}
	filter := initial.Filter
	if view == domain.SourceAll || view == domain.SourceRecommend {
		filter.SourceView = ""
	}
	modelOptions := ModelOptions{}
	if len(options) > 0 {
		modelOptions = options[0]
	}
	styles := themeStylesFor(modelOptions.Config.Theme)
	if strings.TrimSpace(modelOptions.Config.Theme) == "" {
		modelOptions.Config.Theme = styles.spec.value
	}
	modelOptions.Config.GlamourStyle = styles.spec.glamour
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
		service:    service,
		keys:       defaultKeyMap(),
		help:       helpModel,
		config:     modelOptions.Config,
		saveConfig: modelOptions.SaveConfig,
		styles:     styles,
		feedState: feedState{
			view:             view,
			entries:          initial.Entries,
			statuses:         initial.Statuses,
			fetchHistory:     initial.FetchHistory,
			rules:            initial.Rules,
			dedupeCandidates: initial.DedupeCandidates,
			counts:           initial.Counts,
			filter:           filter,
			feedSelections:   make(map[feedSelectionKey]feedSelection),
			activePanel:      panelFeed,
		},
		refreshState: refreshState{
			refreshing:     true,
			refreshID:      1,
			loadingSpinner: spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(styles.active)),
		},
		detailState: detailState{
			images:         imageManager,
			imagePreviewer: imagePreviewer,
			imagePreviews:  make(map[markdownImagePreviewKey]markdownImagePreviewState),
		},
		overlayState: overlayState{
			message: "cached feed loaded",
		},
		width:          100,
		height:         30,
		darkBackground: true,
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
	return batchCommands(
		m.refreshCmd(m.refreshID, false, app.RefreshModeStartup),
		m.refreshProgressCmd(m.refreshID),
		m.loadingSpinner.Tick,
		tea.RequestBackgroundColor,
	)
}

// Update handles TUI messages and key presses.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.handleWindowSizeMsg(msg)
	case tuiimage.RenderedMsg:
		return m.handleRenderedImageMsg(msg)
	case snapshotMsg:
		return m.handleSnapshotMsg(msg)
	case itemStatePatchMsg:
		return m.handleItemStatePatchMsg(msg)
	case refreshProgressMsg:
		return m.handleRefreshProgressMsg(msg)
	case spinner.TickMsg:
		return m.handleSpinnerTickMsg(msg)
	case detailMsg:
		return m.handleDetailMsg(msg)
	case markdownImagePreviewMsg:
		return m.handleMarkdownImagePreviewMsg(msg)
	case recommendDiagnosticsMsg:
		return m.handleRecommendDiagnosticsMsg(msg)
	case detailRawImageRedrawMsg:
		return m.handleDetailRawImageRedrawMsg(msg)
	case statusMsg:
		return m.handleStatusMsg(msg)
	case clearToastMsg:
		return m.handleClearToastMsg(msg)
	case tea.BackgroundColorMsg:
		return m.handleBackgroundColorMsg(msg)
	case tea.MouseClickMsg:
		return m.handleMouseClickMsg(msg)
	case tea.MouseWheelMsg:
		return m.handleMouseWheelMsg(msg)
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}
	if m.overlayIs(overlaySettings) && m.settingsForm != nil {
		return m.updateSettingsForm(msg)
	}
	if m.overlayIs(overlayRuleForm) && m.ruleForm != nil {
		return m.updateRuleForm(msg)
	}
	return m, nil
}

func (m Model) handleWindowSizeMsg(msg tea.WindowSizeMsg) (Model, tea.Cmd) {
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
	if m.overlayIs(overlaySettings) && m.settingsForm != nil {
		m.resizeSettingsForm(msg.Width, msg.Height)
		updated, cmd := m.settingsForm.Update(msg)
		if form, ok := updated.(*huh.Form); ok {
			m.settingsForm = form
		}
		m.ensureSettingsFocusedFieldVisible()
		return m, cmd
	}
	if m.overlayIs(overlayRuleForm) && m.ruleForm != nil {
		m.ruleForm.WithWidth(max(40, msg.Width-4)).WithHeight(max(8, msg.Height-4))
		updated, cmd := m.ruleForm.Update(msg)
		if form, ok := updated.(*huh.Form); ok {
			m.ruleForm = form
		}
		return m, cmd
	}
	return m, batchCommands(imageCmd, m.drawDetailRawImagesCmd())
}

func (m Model) handleRenderedImageMsg(msg tuiimage.RenderedMsg) (Model, tea.Cmd) {
	if m.images != nil {
		m.images.Accept(msg)
	}
	return m, nil
}

func (m Model) handleSnapshotMsg(msg snapshotMsg) (Model, tea.Cmd) {
	if msg.refreshID != 0 && msg.refreshID != m.refreshID {
		m.applyBackgroundSnapshot(msg.snapshot)
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
		m.message = "using cached data; press r to retry"
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
}

func (m Model) handleItemStatePatchMsg(msg itemStatePatchMsg) (Model, tea.Cmd) {
	m.lastError = ""
	m.message = msg.message
	if msg.err != nil {
		m.lastError = msg.err.Error()
		m.message = "item state failed"
		return m, nil
	}
	m.patchEntryState(msg)
	m.clearDetailContentCache()
	if m.overlayIs(overlayDetail) {
		m.refreshDetailContentCache()
	}
	return m, nil
}

func (m *Model) patchEntryState(msg itemStatePatchMsg) {
	now := time.Now().UTC()
	for idx := range m.entries {
		if m.entries[idx].Item.ID != msg.itemID {
			continue
		}
		if m.entries[idx].State.ItemID == "" {
			m.entries[idx].State.ItemID = msg.itemID
		}
		if msg.saved != nil {
			m.entries[idx].State.Saved = *msg.saved
			m.entries[idx].State.SavedAt = stateTimestamp(*msg.saved, now)
		}
		if msg.read != nil {
			m.entries[idx].State.Read = *msg.read
			m.entries[idx].State.ReadAt = stateTimestamp(*msg.read, now)
		}
		if msg.hidden != nil {
			m.entries[idx].State.Hidden = *msg.hidden
			m.entries[idx].State.HiddenAt = stateTimestamp(*msg.hidden, now)
		}
	}
}

func stateTimestamp(enabled bool, now time.Time) *time.Time {
	if !enabled {
		return nil
	}
	return &now
}

func (m Model) handleRefreshProgressMsg(msg refreshProgressMsg) (Model, tea.Cmd) {
	if msg.refreshID != m.refreshID {
		if msg.err == nil {
			m.applyBackgroundSnapshot(msg.snapshot)
		}
		return m, nil
	}
	if !m.refreshing {
		return m, nil
	}
	if msg.err == nil {
		m.applySnapshot(msg.snapshot)
	}
	return m, m.refreshProgressCmd(m.refreshID)
}

func (m Model) handleSpinnerTickMsg(msg spinner.TickMsg) (Model, tea.Cmd) {
	if !m.refreshing && !m.detailLoading {
		return m, nil
	}
	var cmd tea.Cmd
	m.loadingSpinner, cmd = m.loadingSpinner.Update(msg)
	if m.detailLoading {
		m.clearDetailContentCache()
	}
	return m, cmd
}

func (m Model) handleDetailMsg(msg detailMsg) (Model, tea.Cmd) {
	if msg.itemID != m.detailEntryID {
		return m, nil
	}
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

func (m Model) handleMarkdownImagePreviewMsg(msg markdownImagePreviewMsg) (Model, tea.Cmd) {
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
}

func (m Model) handleRecommendDiagnosticsMsg(msg recommendDiagnosticsMsg) (Model, tea.Cmd) {
	m.recommendDiagnosticsLoading = false
	if msg.err != nil {
		m.recommendDiagnostics = domain.RecommendationDiagnostics{}
		m.recommendDiagnosticsError = msg.err.Error()
		m.message = "recommend diagnostics unavailable"
		return m, nil
	}
	m.recommendDiagnostics = msg.diagnostics
	m.recommendDiagnosticsError = ""
	m.message = "recommend diagnostics"
	return m, nil
}

func (m Model) handleDetailRawImageRedrawMsg(msg detailRawImageRedrawMsg) (Model, tea.Cmd) {
	if msg.drawID != m.detailRawImageDrawID ||
		msg.itemID != m.detailEntryID ||
		msg.detailOffset != m.detailOffset ||
		msg.imageVersion != m.detailImageVersion {
		return m, nil
	}
	return m, m.drawDetailRawImagesCmd()
}

func (m Model) handleStatusMsg(msg statusMsg) (Model, tea.Cmd) {
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
}

func (m Model) handleClearToastMsg(msg clearToastMsg) (Model, tea.Cmd) {
	if int(msg) == m.toastID {
		m.toast = ""
	}
	return m, nil
}

func (m Model) handleBackgroundColorMsg(msg tea.BackgroundColorMsg) (Model, tea.Cmd) {
	m.darkBackground = msg.IsDark()
	m.terminalBackground = msg.Color
	m.clearDetailContentCache()
	m.refreshDetailContentCache()
	m.clampDetailOffset()
	return m, nil
}

func (m Model) handleMouseClickMsg(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if overlay := m.activeMouseOverlay(); overlay != overlayNone {
		return m.handleOverlayMouseClick(overlay, msg)
	}
	if m.mainPanelsVisible() {
		return m.handleMouseClick(msg)
	}
	return m, nil
}

func (m Model) handleMouseWheelMsg(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	if m.overlayIs(overlaySettings) {
		return m.handleSettingsMouseWheel(msg)
	}
	if m.overlayIs(overlayDetail) {
		return m.handleDetailMouseWheel(msg)
	}
	if m.mainPanelsVisible() {
		return m.handleMouseWheel(msg)
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
		if m.overlayIs(overlayDetail) {
			m.closeOverlay()
		}
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
	if m.overlayIs(overlayDetail) {
		m.refreshDetailContentCache()
		m.clampDetailOffset()
	}
}

func (m *Model) applyBackgroundSnapshot(snapshot app.Snapshot) {
	m.statuses = mergeBackgroundStatuses(m.statuses, snapshot.Statuses, m.view)
	m.counts = mergeBackgroundCounts(m.counts, snapshot.Counts, m.view)
}

func mergeBackgroundStatuses(current, incoming []domain.SourceHealth, active domain.SourceID) []domain.SourceHealth {
	if len(incoming) == 0 {
		return current
	}
	if active == "" || active == domain.SourceAll {
		if len(current) > 0 {
			return current
		}
		return incoming
	}
	incomingBySource := make(map[domain.SourceID]domain.SourceHealth, len(incoming))
	for _, status := range incoming {
		incomingBySource[status.Source] = status
	}
	merged := make([]domain.SourceHealth, 0, max(len(current), len(incoming)))
	seen := make(map[domain.SourceID]bool, max(len(current), len(incoming)))
	for _, status := range current {
		if status.Source == active {
			merged = append(merged, status)
		} else if next, ok := incomingBySource[status.Source]; ok {
			merged = append(merged, next)
		} else {
			merged = append(merged, status)
		}
		seen[status.Source] = true
	}
	for _, status := range incoming {
		if status.Source == active || seen[status.Source] {
			continue
		}
		merged = append(merged, status)
	}
	return merged
}

func mergeBackgroundCounts(current, incoming map[domain.SourceID]int, active domain.SourceID) map[domain.SourceID]int {
	if len(incoming) == 0 {
		return current
	}
	if active == "" || active == domain.SourceAll {
		if len(current) > 0 {
			return current
		}
		return incoming
	}
	merged := make(map[domain.SourceID]int, max(len(current), len(incoming)))
	for source, count := range current {
		merged[source] = count
	}
	for source, count := range incoming {
		if source == domain.SourceAll || source == active {
			continue
		}
		merged[source] = count
	}
	total := 0
	for _, source := range app.SourceIDs() {
		total += merged[source]
	}
	merged[domain.SourceAll] = total
	return merged
}

func (m Model) mainPanelsVisible() bool {
	return !m.hasBlockingOverlay()
}
