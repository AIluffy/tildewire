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
	if m.mode == inputModeSearch {
		return overlaySearch
	}
	return m.overlay
}

func (m Model) activeMouseOverlay() overlayMode {
	switch m.overlay {
	case overlaySettings:
		return overlaySettings
	case overlayDetail:
		return overlayDetail
	default:
		return overlayNone
	}
}

func (m Model) hasBlockingOverlay() bool {
	switch m.activeOverlay() {
	case overlayNone, overlaySearch:
		return false
	default:
		return true
	}
}

func (m Model) overlayIs(mode overlayMode) bool {
	return m.activeOverlay() == mode
}

func (m *Model) openOverlay(mode overlayMode) {
	m.closeOverlayStateFor(mode)
	m.overlay = mode
}

func (m *Model) closeOverlay() {
	m.closeOverlayStateFor(overlayNone)
	m.overlay = overlayNone
}

func (m *Model) closeOverlayStateFor(next overlayMode) {
	if next != overlaySettings {
		m.settingsForm = nil
		m.settingsScrollOffset = 0
	}
	if next != overlayRuleForm {
		m.ruleForm = nil
		m.ruleEditingID = 0
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
