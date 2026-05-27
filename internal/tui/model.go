package tui

import (
	"image/color"
	"strings"

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
		view = domain.SourceAll
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
			filter:           initial.Filter,
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
	case refreshProgressMsg:
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
	for source, count := range merged {
		if source != domain.SourceAll {
			total += count
		}
	}
	merged[domain.SourceAll] = total
	return merged
}

func (m Model) mainPanelsVisible() bool {
	return !m.settingsOpen && m.ruleForm == nil && !m.filterOpen && !m.health && !m.rulesOpen && !m.dedupeOpen && !m.detail && !m.paletteOpen
}
