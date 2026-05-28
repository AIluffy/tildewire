package tui

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/AIluffy/tildewire/internal/app"
	"github.com/AIluffy/tildewire/internal/config"
	"github.com/AIluffy/tildewire/internal/domain"
)

type settingsDraft struct {
	Theme                string
	GlamourStyle         string
	MarkdownImagePreview string
	HTTPCacheTTLHours    string
	AccessibleForms      bool
	EnabledSources       []string
	GitHubToken          string
	ProductHuntToken     string
}

type settingsOption struct {
	Label string
	Value string
}

var markdownImagePreviewSettingsOptions = []settingsOption{
	{Label: "Auto", Value: config.MarkdownImagePreviewAuto},
	{Label: "Off", Value: config.MarkdownImagePreviewOff},
	{Label: "Kitty", Value: config.MarkdownImagePreviewKitty},
	{Label: "iTerm2", Value: config.MarkdownImagePreviewITerm},
	{Label: "Sixel", Value: config.MarkdownImagePreviewSixel},
	{Label: "Halfblocks", Value: config.MarkdownImagePreviewHalfblocks},
}

func (m Model) handleSettingsKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Back) {
		m.closeSettings("settings cancelled")
		return m, nil
	}
	switch msg.String() {
	case "pgup":
		m.scrollSettings(-m.settingsPageSize())
		return m, nil
	case "pgdown":
		m.scrollSettings(m.settingsPageSize())
		return m, nil
	}
	return m.updateSettingsForm(msg)
}

func (m Model) handleSettingsMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if msg.Mouse().Button != tea.MouseLeft {
		return m, nil
	}
	return m.updateSettingsForm(msg)
}

func (m Model) handleSettingsMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	switch msg.Mouse().Button {
	case tea.MouseWheelUp:
		m.scrollSettings(-3)
	case tea.MouseWheelDown:
		m.scrollSettings(3)
	}
	return m, nil
}

func (m Model) updateSettingsForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.settingsForm == nil {
		return m, nil
	}
	updated, cmd := m.settingsForm.Update(msg)
	if form, ok := updated.(*huh.Form); ok {
		m.settingsForm = form
	}
	m.ensureSettingsFocusedFieldVisible()
	return m.applySettingsFormState(cmd)
}

func (m Model) applySettingsFormState(cmd tea.Cmd) (tea.Model, tea.Cmd) {
	if m.settingsForm == nil {
		return m, cmd
	}
	switch m.settingsForm.State {
	case huh.StateCompleted:
		return m.applySettingsState(cmd)
	case huh.StateAborted:
		m.closeSettings("settings cancelled")
		return m, cmd
	default:
		return m, cmd
	}
}

func (m Model) applySettingsState(cmd tea.Cmd) (tea.Model, tea.Cmd) {
	next := m.config
	styles := themeStylesFor(m.settingsDraft.Theme)
	next.Theme = styles.spec.value
	next.GlamourStyle = styles.spec.glamour
	next.MarkdownImagePreview = normalizeSettingsMarkdownImagePreview(m.settingsDraft.MarkdownImagePreview)
	cacheTTLHours, err := parsePositiveHours(m.settingsDraft.HTTPCacheTTLHours)
	if err != nil {
		m.message = "settings invalid"
		return m, cmd
	}
	next.HTTPCacheTTLHours = cacheTTLHours
	next.AccessibleForms = m.settingsDraft.AccessibleForms
	enabledSources := normalizeSettingsEnabledSources(m.settingsDraft.EnabledSources)
	if len(enabledSources) == 0 {
		m.message = "settings invalid"
		return m, cmd
	}
	next.EnabledSources = enabledSources
	next.GitHubToken = strings.TrimSpace(m.settingsDraft.GitHubToken)
	next.ProductHuntToken = strings.TrimSpace(m.settingsDraft.ProductHuntToken)
	m.service.SetHTTPCacheTTL(next.HTTPCacheTTL())
	m.service.SetSourceConfig(sourceIDsFromStrings(next.EnabledSources), sourceTokensFromConfig(next))
	m.config = next
	m.styles = styles
	m.loadingSpinner = spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(styles.active))
	m.clearDetailContentCache()
	m.refreshDetailContentCache()
	if !m.sourceEnabled(m.view) {
		m.setSource(domain.SourceAll)
	}
	m.closeOverlay()
	m.message = "settings saved"
	return m, batchCommands(cmd, m.saveSettingsCmd(next), m.loadCmd())
}

func (m *Model) openSettingsForm() {
	m.openOverlay(overlaySettings)
	m.settingsDraft = settingsDraft{
		Theme:                themeStylesFor(m.config.Theme).spec.value,
		GlamourStyle:         m.config.GlamourStyle,
		MarkdownImagePreview: normalizeSettingsMarkdownImagePreview(m.config.MarkdownImagePreview),
		HTTPCacheTTLHours:    strconv.Itoa(cacheTTLHoursOrDefault(m.config.HTTPCacheTTLHours)),
		AccessibleForms:      m.config.AccessibleForms,
		EnabledSources:       settingsEnabledSourcesOrDefault(m.config.EnabledSources),
		GitHubToken:          m.config.GitHubToken,
		ProductHuntToken:     m.config.ProductHuntToken,
	}
	if strings.TrimSpace(m.settingsDraft.Theme) == "" {
		m.settingsDraft.Theme = config.ThemeCatppuccin
	}
	m.settingsScrollOffset = 0
	m.settingsForm = m.newSettingsForm()
	_ = m.settingsForm.Init()
}

func (m *Model) closeSettings(message string) {
	m.closeOverlay()
	m.message = message
}

func (m *Model) newSettingsForm() *huh.Form {
	return huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Display").
				Next(false),
			huh.NewSelect[string]().
				Key("settings_theme").
				Title("Theme").
				Options(settingsHuhOptions(themeSettingsOptions())...).
				Value(&m.settingsDraft.Theme),
			huh.NewSelect[string]().
				Key("settings_markdown_image_preview").
				Title("Markdown image preview").
				Options(settingsHuhOptions(markdownImagePreviewSettingsOptions)...).
				Value(&m.settingsDraft.MarkdownImagePreview),
		).Title("Display"),
		huh.NewGroup(
			huh.NewNote().
				Title("Sources").
				Next(false),
			huh.NewMultiSelect[string]().
				Key("settings_visible_sources").
				Title("Visible sources").
				Options(settingsHuhOptions(settingsSourceOptions())...).
				Value(&m.settingsDraft.EnabledSources).
				Validate(validateSettingsSources),
		).Title("Sources"),
		huh.NewGroup(
			huh.NewNote().
				Title("Credentials").
				Next(false),
			huh.NewInput().
				Key("settings_github_token").
				Title("GitHub token").
				EchoMode(huh.EchoModePassword).
				Value(&m.settingsDraft.GitHubToken),
			huh.NewInput().
				Key("settings_producthunt_token").
				Title("Product Hunt token").
				EchoMode(huh.EchoModePassword).
				Value(&m.settingsDraft.ProductHuntToken),
		).Title("Credentials"),
		huh.NewGroup(
			huh.NewNote().
				Title("System").
				Next(false),
			huh.NewInput().
				Key("settings_http_cache_ttl").
				Title("HTTP cache TTL").
				Description("Hours").
				Value(&m.settingsDraft.HTTPCacheTTLHours).
				Validate(validateSettingsTTL),
			huh.NewConfirm().
				Key("settings_accessible_forms").
				Title("Accessible forms").
				Affirmative("Enabled").
				Negative("Disabled").
				Value(&m.settingsDraft.AccessibleForms),
		).Title("System"),
	).
		WithAccessible(m.config.AccessibleForms).
		WithLayout(huh.LayoutStack).
		WithWidth(settingsFormWidth(m.width)).
		WithHeight(settingsFormHeight(m.height))
}

func (m Model) renderSettingsPanel() string {
	width := settingsPanelWidth(m.width)
	form := m.styles.muted.Render("Settings form unavailable.")
	if m.settingsForm != nil {
		form = m.renderSettingsFormViewport(m.settingsForm.View())
	}
	panel := m.styles.panel.Width(width).Render(strings.Join([]string{
		m.styles.header.Render("tildewire SETTINGS"),
		"",
		form,
	}, "\n"))
	return placeBlock(panel, 0, 0, max(40, m.width))
}

func (m *Model) resizeSettingsForm(width, height int) {
	if m.settingsForm == nil {
		return
	}
	m.settingsForm.WithWidth(settingsFormWidth(width)).WithHeight(settingsFormHeight(height))
	m.clampSettingsScrollOffset()
}

func (m Model) renderSettingsFormViewport(form string) string {
	lines := strings.Split(form, "\n")
	visible := settingsFormVisibleRows(m.height)
	offset := clamp(m.settingsScrollOffset, 0, max(0, len(lines)-visible))
	end := min(len(lines), offset+visible)
	if offset < end {
		lines = lines[offset:end]
	} else {
		lines = nil
	}
	return fillLines(lines, visible)
}

func (m *Model) scrollSettings(delta int) {
	m.settingsScrollOffset += delta
	m.clampSettingsScrollOffset()
}

func (m *Model) clampSettingsScrollOffset() {
	m.settingsScrollOffset = clamp(m.settingsScrollOffset, 0, m.maxSettingsScrollOffset())
}

func (m Model) maxSettingsScrollOffset() int {
	if m.settingsForm == nil {
		return 0
	}
	lines := strings.Split(m.settingsForm.View(), "\n")
	return max(0, len(lines)-settingsFormVisibleRows(m.height))
}

func (m Model) settingsPageSize() int {
	return max(1, settingsFormVisibleRows(m.height)-1)
}

func (m *Model) ensureSettingsFocusedFieldVisible() {
	if m.settingsForm == nil {
		return
	}
	line, ok := m.settingsFocusedFieldLine()
	if !ok {
		m.clampSettingsScrollOffset()
		return
	}
	visible := settingsFormVisibleRows(m.height)
	if line < m.settingsScrollOffset {
		m.settingsScrollOffset = line
	} else if line >= m.settingsScrollOffset+visible {
		m.settingsScrollOffset = line - visible + 1
	}
	m.clampSettingsScrollOffset()
}

func (m Model) settingsFocusedFieldLine() (int, bool) {
	field := m.settingsForm.GetFocusedField()
	if field == nil {
		return 0, false
	}
	title, ok := settingsFieldTitles[field.GetKey()]
	if !ok {
		return 0, false
	}
	for idx, line := range strings.Split(m.settingsForm.View(), "\n") {
		if strings.Contains(line, title) {
			return idx, true
		}
	}
	return 0, false
}

func settingsPanelWidth(width int) int {
	return max(48, width-4)
}

func settingsFormWidth(width int) int {
	return max(40, width-4)
}

func settingsFormHeight(height int) int {
	return settingsFormVisibleRows(height)
}

func settingsFormVisibleRows(height int) int {
	return max(8, max(12, height)-4)
}

var settingsFieldTitles = map[string]string{
	"settings_theme":                  "Theme",
	"settings_markdown_image_preview": "Markdown image preview",
	"settings_visible_sources":        "Visible sources",
	"settings_github_token":           "GitHub token",
	"settings_producthunt_token":      "Product Hunt token",
	"settings_http_cache_ttl":         "HTTP cache TTL",
	"settings_accessible_forms":       "Accessible forms",
}

func settingsHuhOptions(options []settingsOption) []huh.Option[string] {
	huhOptions := make([]huh.Option[string], 0, len(options))
	for _, option := range options {
		huhOptions = append(huhOptions, huh.NewOption(option.Label, option.Value))
	}
	return huhOptions
}

func settingsSourceOptions() []settingsOption {
	catalog := app.SourceCatalog()
	options := make([]settingsOption, 0, len(catalog))
	for _, source := range catalog {
		options = append(options, settingsOption{Label: source.Label, Value: string(source.Source)})
	}
	return options
}

func validateSettingsSources(values []string) error {
	if len(normalizeSettingsEnabledSources(values)) == 0 {
		return fmt.Errorf("at least one source is required")
	}
	return nil
}

func validateSettingsTTL(value string) error {
	_, err := parsePositiveHours(value)
	return err
}

func (m Model) saveSettingsCmd(cfg config.Config) tea.Cmd {
	return m.saveConfigCmd(cfg, "settings saved", "save settings failed")
}

func (m Model) saveSourceOrderCmd(cfg config.Config) tea.Cmd {
	return m.saveConfigCmd(cfg, "source order saved", "save source order failed")
}

func (m Model) saveConfigCmd(cfg config.Config, successMessage, failureMessage string) tea.Cmd {
	return func() tea.Msg {
		if m.saveConfig == nil {
			return statusMsg{message: successMessage}
		}
		if err := m.saveConfig(cfg); err != nil {
			return statusMsg{message: failureMessage, err: err}
		}
		return statusMsg{message: successMessage}
	}
}

func cacheTTLHoursOrDefault(value int) int {
	if value <= 0 {
		return 6
	}
	return value
}

func normalizeSettingsMarkdownImagePreview(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
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

func parsePositiveHours(value string) (int, error) {
	hours, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || hours <= 0 {
		return 0, fmt.Errorf("hours must be a positive integer")
	}
	return hours, nil
}

func normalizeSettingsEnabledSources(values []string) []string {
	allowed := make(map[string]bool)
	for _, source := range app.SourceIDs() {
		allowed[string(source)] = true
	}
	seen := make(map[string]bool, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || !allowed[value] || seen[value] {
			continue
		}
		seen[value] = true
		normalized = append(normalized, value)
	}
	return normalized
}

func settingsEnabledSourcesOrDefault(values []string) []string {
	normalized := normalizeSettingsEnabledSources(values)
	if len(normalized) == 0 {
		return sourceStrings(app.SourceIDs())
	}
	return normalized
}

func sourceIDsFromStrings(values []string) []domain.SourceID {
	values = normalizeSettingsEnabledSources(values)
	sources := make([]domain.SourceID, 0, len(values))
	for _, value := range values {
		sources = append(sources, domain.SourceID(value))
	}
	return sources
}

func sourceTokensFromConfig(cfg config.Config) map[domain.SourceID]string {
	return map[domain.SourceID]string{
		domain.SourceGitHub:      cfg.GitHubToken,
		domain.SourceProductHunt: cfg.ProductHuntToken,
	}
}

func sourceStrings(sources []domain.SourceID) []string {
	values := make([]string, 0, len(sources))
	for _, source := range sources {
		values = append(values, string(source))
	}
	return values
}
