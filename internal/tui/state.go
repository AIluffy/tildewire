package tui

import (
	"charm.land/bubbles/v2/spinner"
	"charm.land/huh/v2"

	"github.com/AIluffy/tildewire/internal/app"
	"github.com/AIluffy/tildewire/internal/domain"
	tuiimage "github.com/AIluffy/tildewire/internal/tui/image"
)

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

type feedState struct {
	view             domain.SourceID
	entries          []domain.FeedEntry
	statuses         []domain.SourceHealth
	fetchHistory     []domain.FetchEvent
	rules            []domain.PersonalizationRule
	dedupeCandidates []domain.DedupeCandidate
	counts           map[domain.SourceID]int
	filter           app.FeedFilter
	mode             inputMode
	searchDraft      string
	searchBase       string
	searchLoadID     int
	cursor           int
	previewOffset    int
	feedOffset       int
	feedSelections   map[feedSelectionKey]feedSelection
	sourcesOffset    int
	activePanel      mainPanel
}

type refreshState struct {
	refreshing     bool
	refreshID      int
	loadingSpinner spinner.Model
}

type detailState struct {
	detail               bool
	detailLoading        bool
	detailEntryID        string
	itemDetail           domain.ItemDetail
	detailError          string
	detailOffset         int
	detailLineCache      detailLineCache
	detailImageVersion   int
	detailRawImageDrawID int
	images               *tuiimage.ImageManager
	imagePreviewer       markdownImagePreviewer
	imagePreviews        map[markdownImagePreviewKey]markdownImagePreviewState
}

type overlayState struct {
	paletteOpen                 bool
	paletteFilter               string
	paletteCursor               int
	paletteOffset               int
	filterOpen                  bool
	filterDraft                 app.FeedFilter
	filterDraftView             domain.SourceID
	filterCursor                int
	health                      bool
	rulesOpen                   bool
	ruleCursor                  int
	ruleForm                    *huh.Form
	ruleDraft                   ruleDraft
	ruleEditingID               int64
	dedupeOpen                  bool
	dedupeCursor                int
	recommendDiagnosticsOpen    bool
	recommendDiagnosticsLoading bool
	recommendDiagnostics        domain.RecommendationDiagnostics
	recommendDiagnosticsError   string
	message                     string
	lastError                   string
	toast                       string
	toastID                     int
}

type settingsState struct {
	settingsOpen                 bool
	settingsCursor               int
	settingsSourceCursor         int
	settingsShowGitHubToken      bool
	settingsShowProductHuntToken bool
	settingsDraft                settingsDraft
}
