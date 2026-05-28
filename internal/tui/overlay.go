package tui

import tea "charm.land/bubbletea/v2"

type overlayMode int

const (
	overlayNone overlayMode = iota
	overlaySettings
	overlayRuleForm
	overlayPalette
	overlayFilter
	overlayRules
	overlayDedupe
	overlayRecommendDiagnostics
	overlayHealth
	overlaySearch
	overlayDetail
)

func (m Model) activeOverlay() overlayMode {
	switch {
	case m.settingsOpen:
		return overlaySettings
	case m.ruleForm != nil:
		return overlayRuleForm
	case m.paletteOpen:
		return overlayPalette
	case m.filterOpen:
		return overlayFilter
	case m.rulesOpen:
		return overlayRules
	case m.dedupeOpen:
		return overlayDedupe
	case m.recommendDiagnosticsOpen:
		return overlayRecommendDiagnostics
	case m.health:
		return overlayHealth
	case m.mode == inputModeSearch:
		return overlaySearch
	case m.detail:
		return overlayDetail
	default:
		return overlayNone
	}
}

func (m Model) activeMouseOverlay() overlayMode {
	switch {
	case m.settingsOpen:
		return overlaySettings
	case m.detail:
		return overlayDetail
	default:
		return overlayNone
	}
}

func (m Model) handleOverlayKey(mode overlayMode, msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch mode {
	case overlaySettings:
		return m.handleSettingsKey(msg)
	case overlayRuleForm:
		return m.handleRuleFormKey(msg)
	case overlayPalette:
		return m.handlePaletteKey(msg)
	case overlayFilter:
		return m.handleFilterKey(msg)
	case overlayRules:
		return m.handleRulesKey(msg)
	case overlayDedupe:
		return m.handleDedupeKey(msg)
	case overlayRecommendDiagnostics:
		return m.handleRecommendDiagnosticsKey(msg)
	case overlayHealth:
		return m.handleHealthKey(msg)
	case overlaySearch:
		return m.handleSearchKey(msg)
	case overlayDetail:
		return m.handleDetailKey(msg)
	default:
		return m, nil
	}
}

func (m Model) handleOverlayMouseClick(mode overlayMode, msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	switch mode {
	case overlaySettings:
		return m.handleSettingsMouseClick(msg)
	case overlayDetail:
		return m.handleDetailMouseClick(msg)
	default:
		return m, nil
	}
}
