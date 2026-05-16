package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/zhangxueai/tildewire/internal/app"
	"github.com/zhangxueai/tildewire/internal/config"
	"github.com/zhangxueai/tildewire/internal/domain"
)

type settingsDraft struct {
	GlamourStyle         string
	MarkdownImagePreview string
	HTTPCacheTTLHours    string
	AccessibleForms      bool
	EnabledSources       []string
	GitHubToken          string
	ProductHuntToken     string
}

type httpCacheTTLSetter interface {
	SetHTTPCacheTTL(time.Duration)
}

type settingsField int

const (
	settingsFieldNone settingsField = iota
	settingsFieldMarkdownStyle
	settingsFieldMarkdownImagePreview
	settingsFieldVisibleSources
	settingsFieldGitHubToken
	settingsFieldProductHuntToken
	settingsFieldHTTPCacheTTL
	settingsFieldAccessibleForms
	settingsFieldSave
)

type settingsAction int

const (
	settingsActionNone settingsAction = iota
	settingsActionSave
	settingsActionCancel
	settingsActionToggleSecret
)

type settingsOption struct {
	Label string
	Value string
}

type settingsHitSpan struct {
	start  int
	end    int
	field  settingsField
	value  string
	action settingsAction
}

type settingsRenderRow struct {
	field settingsField
	line  string
	spans []settingsHitSpan
}

type settingsHit struct {
	field  settingsField
	value  string
	action settingsAction
}

type settingsPanelBounds struct {
	x             int
	y             int
	width         int
	height        int
	contentX      int
	contentY      int
	contentWidth  int
	contentHeight int
}

type settingsLineBuilder struct {
	line  string
	cell  int
	spans []settingsHitSpan
}

var settingsFocusableFields = []settingsField{
	settingsFieldMarkdownStyle,
	settingsFieldMarkdownImagePreview,
	settingsFieldVisibleSources,
	settingsFieldGitHubToken,
	settingsFieldProductHuntToken,
	settingsFieldHTTPCacheTTL,
	settingsFieldAccessibleForms,
	settingsFieldSave,
}

var markdownStyleSettingsOptions = []settingsOption{
	{Label: "Dark", Value: "dark"},
	{Label: "Light", Value: "light"},
	{Label: "Notty", Value: "notty"},
	{Label: "ASCII", Value: "ascii"},
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
	switch {
	case key.Matches(msg, m.keys.Back):
		m.closeSettings("settings cancelled")
		return m, nil
	case key.Matches(msg, m.keys.Up):
		m.moveSettingsCursor(-1)
		return m, nil
	case key.Matches(msg, m.keys.Down):
		m.moveSettingsCursor(1)
		return m, nil
	case key.Matches(msg, m.keys.FocusLeft):
		m.changeSelectedSetting(-1)
		return m, nil
	case key.Matches(msg, m.keys.FocusRight):
		m.changeSelectedSetting(1)
		return m, nil
	case key.Matches(msg, m.keys.OpenDetail):
		return m.applySettingsState(nil)
	}

	switch msg.String() {
	case "space":
		return m.activateSelectedSetting()
	case "ctrl+u":
		m.clearSelectedSettingText()
		return m, nil
	case "backspace", "ctrl+h":
		m.deleteSelectedSettingRune()
		return m, nil
	}
	if text := settingsInputText(msg); text != "" {
		m.appendSelectedSettingText(text)
	}
	return m, nil
}

func (m Model) handleSettingsMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	mouse := msg.Mouse()
	if mouse.Button != tea.MouseLeft {
		return m, nil
	}
	hit, ok := m.settingsHitAt(mouse.X, mouse.Y)
	if !ok {
		return m, nil
	}
	if hit.field != settingsFieldNone {
		m.focusSettingsField(hit.field)
	}
	if hit.value != "" {
		m.applySettingsValue(hit.field, hit.value)
	}
	switch hit.action {
	case settingsActionSave:
		return m.applySettingsState(nil)
	case settingsActionCancel:
		m.closeSettings("settings cancelled")
	case settingsActionToggleSecret:
		m.toggleSettingsSecretVisibility(hit.field)
	}
	return m, nil
}

func (m Model) applySettingsState(cmd tea.Cmd) (tea.Model, tea.Cmd) {
	next := m.config
	next.GlamourStyle = strings.TrimSpace(m.settingsDraft.GlamourStyle)
	if next.GlamourStyle == "" {
		next.GlamourStyle = "dark"
	}
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
	if setter, ok := m.service.(httpCacheTTLSetter); ok {
		setter.SetHTTPCacheTTL(next.HTTPCacheTTL())
	}
	m.service.SetSourceConfig(sourceIDsFromStrings(next.EnabledSources), sourceTokensFromConfig(next))
	m.config = next
	if !m.sourceEnabled(m.view) {
		m.setSource(domain.SourceAll)
	}
	m.settingsOpen = false
	m.message = "settings saved"
	return m, tea.Batch(cmd, m.saveSettingsCmd(next))
}

func (m *Model) openSettingsForm() {
	m.settingsDraft = settingsDraft{
		GlamourStyle:         m.config.GlamourStyle,
		MarkdownImagePreview: normalizeSettingsMarkdownImagePreview(m.config.MarkdownImagePreview),
		HTTPCacheTTLHours:    strconv.Itoa(cacheTTLHoursOrDefault(m.config.HTTPCacheTTLHours)),
		AccessibleForms:      m.config.AccessibleForms,
		EnabledSources:       settingsEnabledSourcesOrDefault(m.config.EnabledSources),
		GitHubToken:          m.config.GitHubToken,
		ProductHuntToken:     m.config.ProductHuntToken,
	}
	if strings.TrimSpace(m.settingsDraft.GlamourStyle) == "" {
		m.settingsDraft.GlamourStyle = "dark"
	}
	m.settingsOpen = true
	m.settingsShowGitHubToken = false
	m.settingsShowProductHuntToken = false
	if m.settingsCursor < 0 || m.settingsCursor >= len(settingsFocusableFields) {
		m.settingsCursor = 0
	}
	m.clampSettingsSourceCursor()
}

func (m *Model) closeSettings(message string) {
	m.settingsOpen = false
	m.message = message
}

func (m *Model) moveSettingsCursor(delta int) {
	if len(settingsFocusableFields) == 0 {
		m.settingsCursor = 0
		return
	}
	m.settingsCursor = clamp(m.settingsCursor+delta, 0, len(settingsFocusableFields)-1)
}

func (m Model) selectedSettingsField() settingsField {
	if m.settingsCursor < 0 || m.settingsCursor >= len(settingsFocusableFields) {
		return settingsFocusableFields[0]
	}
	return settingsFocusableFields[m.settingsCursor]
}

func (m *Model) focusSettingsField(field settingsField) {
	for idx, candidate := range settingsFocusableFields {
		if candidate == field {
			m.settingsCursor = idx
			return
		}
	}
}

func (m *Model) changeSelectedSetting(delta int) {
	switch m.selectedSettingsField() {
	case settingsFieldMarkdownStyle:
		m.settingsDraft.GlamourStyle = cycleSettingsOption(markdownStyleSettingsOptions, m.settingsDraft.GlamourStyle, delta)
	case settingsFieldMarkdownImagePreview:
		m.settingsDraft.MarkdownImagePreview = cycleSettingsOption(markdownImagePreviewSettingsOptions, m.settingsDraft.MarkdownImagePreview, delta)
	case settingsFieldVisibleSources:
		m.settingsSourceCursor = clamp(m.settingsSourceCursor+delta, 0, len(settingsSourceOptions())-1)
	case settingsFieldHTTPCacheTTL:
		hours, err := parsePositiveHours(m.settingsDraft.HTTPCacheTTLHours)
		if err != nil {
			hours = cacheTTLHoursOrDefault(0)
		}
		m.settingsDraft.HTTPCacheTTLHours = strconv.Itoa(max(1, hours+delta))
	case settingsFieldAccessibleForms:
		m.settingsDraft.AccessibleForms = !m.settingsDraft.AccessibleForms
	}
}

func (m Model) activateSelectedSetting() (tea.Model, tea.Cmd) {
	switch m.selectedSettingsField() {
	case settingsFieldMarkdownStyle, settingsFieldMarkdownImagePreview, settingsFieldAccessibleForms:
		m.changeSelectedSetting(1)
	case settingsFieldVisibleSources:
		m.toggleSettingsSourceAtCursor()
	case settingsFieldGitHubToken, settingsFieldProductHuntToken:
		m.toggleSettingsSecretVisibility(m.selectedSettingsField())
	case settingsFieldSave:
		return m.applySettingsState(nil)
	}
	return m, nil
}

func settingsInputText(msg tea.KeyPressMsg) string {
	if text := msg.Key().Text; text != "" {
		return text
	}
	value := msg.String()
	if len([]rune(value)) == 1 {
		return value
	}
	return ""
}

func (m *Model) appendSelectedSettingText(text string) {
	switch m.selectedSettingsField() {
	case settingsFieldGitHubToken:
		m.settingsDraft.GitHubToken += text
	case settingsFieldProductHuntToken:
		m.settingsDraft.ProductHuntToken += text
	case settingsFieldHTTPCacheTTL:
		for _, r := range text {
			if r >= '0' && r <= '9' {
				m.settingsDraft.HTTPCacheTTLHours += string(r)
			}
		}
	}
}

func (m *Model) clearSelectedSettingText() {
	switch m.selectedSettingsField() {
	case settingsFieldGitHubToken:
		m.settingsDraft.GitHubToken = ""
	case settingsFieldProductHuntToken:
		m.settingsDraft.ProductHuntToken = ""
	case settingsFieldHTTPCacheTTL:
		m.settingsDraft.HTTPCacheTTLHours = ""
	}
}

func (m *Model) deleteSelectedSettingRune() {
	switch m.selectedSettingsField() {
	case settingsFieldGitHubToken:
		m.settingsDraft.GitHubToken = dropLastRune(m.settingsDraft.GitHubToken)
	case settingsFieldProductHuntToken:
		m.settingsDraft.ProductHuntToken = dropLastRune(m.settingsDraft.ProductHuntToken)
	case settingsFieldHTTPCacheTTL:
		m.settingsDraft.HTTPCacheTTLHours = dropLastRune(m.settingsDraft.HTTPCacheTTLHours)
	}
}

func (m *Model) applySettingsValue(field settingsField, value string) {
	switch field {
	case settingsFieldMarkdownStyle:
		m.settingsDraft.GlamourStyle = value
	case settingsFieldMarkdownImagePreview:
		m.settingsDraft.MarkdownImagePreview = value
	case settingsFieldAccessibleForms:
		m.settingsDraft.AccessibleForms = value == "true"
	case settingsFieldVisibleSources:
		m.setSettingsSourceCursor(value)
		m.toggleSettingsSourceValue(value)
	}
}

func (m *Model) toggleSettingsSecretVisibility(field settingsField) {
	switch field {
	case settingsFieldGitHubToken:
		m.settingsShowGitHubToken = !m.settingsShowGitHubToken
	case settingsFieldProductHuntToken:
		m.settingsShowProductHuntToken = !m.settingsShowProductHuntToken
	}
}

func (m *Model) toggleSettingsSourceAtCursor() {
	options := settingsSourceOptions()
	if len(options) == 0 {
		return
	}
	m.clampSettingsSourceCursor()
	m.toggleSettingsSourceValue(options[m.settingsSourceCursor].Value)
}

func (m *Model) toggleSettingsSourceValue(value string) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return
	}
	enabled := settingsSourceEnabled(m.settingsDraft.EnabledSources, value)
	next := make([]string, 0, len(m.settingsDraft.EnabledSources)+1)
	for _, source := range normalizeSettingsEnabledSources(m.settingsDraft.EnabledSources) {
		if source != value {
			next = append(next, source)
		}
	}
	if !enabled {
		next = append(next, value)
	}
	m.settingsDraft.EnabledSources = next
}

func (m *Model) setSettingsSourceCursor(value string) {
	for idx, option := range settingsSourceOptions() {
		if option.Value == value {
			m.settingsSourceCursor = idx
			return
		}
	}
}

func (m *Model) clampSettingsSourceCursor() {
	options := settingsSourceOptions()
	if len(options) == 0 {
		m.settingsSourceCursor = 0
		return
	}
	m.settingsSourceCursor = clamp(m.settingsSourceCursor, 0, len(options)-1)
}

func (m Model) settingsHitAt(x, y int) (settingsHit, bool) {
	bounds := m.settingsPanelBounds()
	rows := m.settingsRenderRows(bounds.contentWidth)
	if x < bounds.contentX || x >= bounds.contentX+bounds.contentWidth || y < bounds.contentY || y >= bounds.contentY+len(rows) {
		return settingsHit{}, false
	}
	rowIndex := y - bounds.contentY
	if rowIndex < 0 || rowIndex >= len(rows) {
		return settingsHit{}, false
	}
	row := rows[rowIndex]
	if row.field == settingsFieldNone && len(row.spans) == 0 {
		return settingsHit{}, false
	}
	hit := settingsHit{field: row.field}
	relativeX := x - bounds.contentX
	for _, span := range row.spans {
		if relativeX >= span.start && relativeX < span.end {
			hit.field = span.field
			hit.value = span.value
			hit.action = span.action
			return hit, true
		}
	}
	return hit, true
}

func (m Model) renderSettingsPanel() string {
	bounds := m.settingsPanelBounds()
	rows := m.settingsRenderRows(bounds.contentWidth)
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		lines = append(lines, clip(row.line, bounds.contentWidth))
	}
	help := clip(settingsHelpRow(), bounds.contentWidth)
	if len(lines) >= bounds.contentHeight {
		lines = lines[:bounds.contentHeight]
		lines[bounds.contentHeight-1] = help
	} else {
		for len(lines) < bounds.contentHeight-1 {
			lines = append(lines, "")
		}
		lines = append(lines, help)
	}
	panel := panelStyle.
		Width(bounds.width).
		Height(bounds.contentHeight).
		Render(strings.Join(lines, "\n"))
	return placeBlock(panel, bounds.x, bounds.y, max(40, m.width))
}

func (m Model) settingsPanelBounds() settingsPanelBounds {
	renderWidth := max(40, m.width)
	renderHeight := max(12, m.height)
	width := renderWidth
	height := renderHeight
	x := 0
	y := 0
	left := panelStyle.GetBorderLeftSize() + panelStyle.GetPaddingLeft()
	top := panelStyle.GetBorderTopSize() + panelStyle.GetPaddingTop()
	frameWidth, frameHeight := panelStyle.GetFrameSize()
	return settingsPanelBounds{
		x:             x,
		y:             y,
		width:         width,
		height:        height,
		contentX:      x + left,
		contentY:      y + top,
		contentWidth:  max(1, width-frameWidth),
		contentHeight: max(1, height-frameHeight),
	}
}

func (m Model) settingsRenderRows(width int) []settingsRenderRow {
	rows := []settingsRenderRow{
		{line: headerStyle.Render("tildewire SETTINGS")},
		{line: ""},
		settingsSectionRow("Display"),
	}
	rows = append(rows,
		m.paddedSettingsRow(m.settingsChoiceRow(settingsFieldMarkdownStyle, "Markdown style", markdownStyleSettingsOptions, m.settingsDraft.GlamourStyle, width))...,
	)
	rows = append(rows,
		m.settingsChoiceRow(settingsFieldMarkdownImagePreview, "Markdown image preview", markdownImagePreviewSettingsOptions, m.settingsDraft.MarkdownImagePreview, width),
		settingsDividerRow(width),
	)
	rows = append(rows,
		settingsSectionRow("Sources"),
	)
	rows = append(rows,
		m.paddedSettingsRow(m.settingsSourcesRow(width))...,
	)
	rows = append(rows,
		settingsDividerRow(width),
	)
	rows = append(rows,
		settingsSectionRow("Credentials"),
	)
	rows = append(rows,
		m.paddedSettingsRow(m.settingsSecretRow(settingsFieldGitHubToken, "GitHub token", m.settingsDraft.GitHubToken, m.settingsShowGitHubToken, width))...,
	)
	rows = append(rows,
		m.settingsSecretRow(settingsFieldProductHuntToken, "Product Hunt token", m.settingsDraft.ProductHuntToken, m.settingsShowProductHuntToken, width),
		settingsDividerRow(width),
	)
	rows = append(rows,
		settingsSectionRow("System"),
	)
	rows = append(rows,
		m.paddedSettingsRow(m.settingsTextRow(settingsFieldHTTPCacheTTL, "HTTP cache TTL", settingsHoursLabel(m.settingsDraft.HTTPCacheTTLHours), width))...,
	)
	rows = append(rows,
		m.settingsChoiceRow(settingsFieldAccessibleForms, "Accessible forms", accessibleFormSettingsOptions(), boolSettingsValue(m.settingsDraft.AccessibleForms), width),
	)
	rows = append(rows,
		settingsRenderRow{line: ""},
		settingsRenderRow{line: ""},
		m.settingsActionRow(width),
	)
	return rows
}

func settingsSectionRow(title string) settingsRenderRow {
	return settingsRenderRow{line: headerStyle.Render(title)}
}

func (m Model) paddedSettingsRow(row settingsRenderRow) []settingsRenderRow {
	return []settingsRenderRow{
		{line: ""},
		row,
	}
}

func settingsDividerRow(width int) settingsRenderRow {
	dividerWidth := max(1, width-2)
	return settingsRenderRow{line: mutedStyle.Render("  " + strings.Repeat("─", dividerWidth))}
}

func settingsHelpRow() string {
	parts := []string{
		"j/k move",
		"click",
		"l/r change",
		"space",
		"type",
		"ctrl+u clear",
		"enter save",
		"esc cancel",
	}
	return mutedStyle.Render(strings.Join(parts, " ｜ "))
}

func (m Model) settingsChoiceRow(field settingsField, title string, options []settingsOption, value string, width int) settingsRenderRow {
	focused := m.selectedSettingsField() == field
	builder := newSettingsFieldLine(focused, title, width)
	for idx, option := range options {
		if idx > 0 {
			builder.write(" ")
		}
		selected := option.Value == value
		builder.writeHit(field, option.Value, settingsChoiceChip(option.Label, selected, focused))
	}
	return settingsRenderRow{field: field, line: builder.string(), spans: builder.spans}
}

func (m Model) settingsSourcesRow(width int) settingsRenderRow {
	field := settingsFieldVisibleSources
	focused := m.selectedSettingsField() == field
	builder := newSettingsFieldLine(focused, "Visible sources", width)
	options := settingsSourceOptions()
	for idx, option := range options {
		if idx > 0 {
			builder.write(" ")
		}
		selected := settingsSourceEnabled(m.settingsDraft.EnabledSources, option.Value)
		subfocused := focused && idx == m.settingsSourceCursor
		builder.writeHit(field, option.Value, settingsSourceChip(option.Label, selected, subfocused))
	}
	return settingsRenderRow{field: field, line: builder.string(), spans: builder.spans}
}

func (m Model) settingsTextRow(field settingsField, title string, value string, width int) settingsRenderRow {
	focused := m.selectedSettingsField() == field
	builder := newSettingsFieldLine(focused, title, width)
	if focused {
		value = controlStyle.Render(value)
	} else if strings.TrimSpace(value) == "(empty)" {
		value = mutedStyle.Render(value)
	}
	builder.write(value)
	return settingsRenderRow{field: field, line: builder.string()}
}

func (m Model) settingsSecretRow(field settingsField, title string, value string, show bool, width int) settingsRenderRow {
	focused := m.selectedSettingsField() == field
	builder := newSettingsFieldLine(focused, title, width)
	display := maskSettingsSecret(value)
	if show {
		display = emptySettingsValue(value)
	}
	if focused {
		display = controlStyle.Render(display)
	} else if strings.TrimSpace(display) == "(empty)" {
		display = mutedStyle.Render(display)
	}
	builder.write(display)
	builder.write("  ")
	builder.writeHit(field, "", settingsSecretToggleButton(focused || show), settingsActionToggleSecret)
	return settingsRenderRow{field: field, line: builder.string(), spans: builder.spans}
}

func (m Model) settingsActionRow(width int) settingsRenderRow {
	focused := m.selectedSettingsField() == settingsFieldSave
	builder := settingsLineBuilder{}
	save := "Save settings"
	if focused {
		save = activeStyle.Render("> " + save)
	} else {
		save = controlStyle.Render(save)
	}
	builder.writeHit(settingsFieldSave, "", save, settingsActionSave)
	builder.write("   ")
	builder.writeHit(settingsFieldNone, "", mutedStyle.Render("Cancel"), settingsActionCancel)
	offset := max(0, width-builder.cell-2)
	return settingsRenderRow{field: settingsFieldSave, line: strings.Repeat(" ", offset) + clip(builder.string(), width-offset), spans: offsetSettingsHitSpans(builder.spans, offset)}
}

func offsetSettingsHitSpans(spans []settingsHitSpan, offset int) []settingsHitSpan {
	if offset == 0 {
		return spans
	}
	next := make([]settingsHitSpan, len(spans))
	for idx, span := range spans {
		span.start += offset
		span.end += offset
		next[idx] = span
	}
	return next
}

func newSettingsFieldLine(focused bool, title string, width int) settingsLineBuilder {
	labelWidth := settingsLabelWidth(width)
	builder := settingsLineBuilder{}
	if focused {
		builder.write(activeStyle.Render("> "))
		builder.write(activeStyle.Render(fmt.Sprintf("%-*s", labelWidth, title)))
	} else {
		builder.write("  ")
		builder.write(fmt.Sprintf("%-*s", labelWidth, title))
	}
	builder.write(" ")
	return builder
}

func settingsLabelWidth(width int) int {
	if width < 72 {
		return 21
	}
	return 25
}

func (b *settingsLineBuilder) write(value string) {
	b.line += value
	b.cell += ansi.StringWidth(value)
}

func (b *settingsLineBuilder) writeHit(field settingsField, value string, rendered string, actions ...settingsAction) {
	action := settingsActionNone
	if len(actions) > 0 {
		action = actions[0]
	}
	start := b.cell
	b.write(rendered)
	b.spans = append(b.spans, settingsHitSpan{
		start:  start,
		end:    b.cell,
		field:  field,
		value:  value,
		action: action,
	})
}

func (b *settingsLineBuilder) string() string {
	return b.line
}

func settingsChoiceChip(label string, selected bool, focused bool) string {
	chip := "[" + label + "]"
	if selected {
		if focused {
			return activeStyle.Render(chip)
		}
		return controlStyle.Render(chip)
	}
	return mutedStyle.Render(chip)
}

func settingsSourceChip(label string, selected bool, focused bool) string {
	marker := " "
	if selected {
		marker = "x"
	}
	chip := "[" + marker + "] " + label
	if focused {
		return activeStyle.Render(chip)
	}
	if selected {
		return okStyle.Render(chip)
	}
	return mutedStyle.Render(chip)
}

func settingsHoursLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "(empty)"
	}
	return value + " hours"
}

func maskSettingsSecret(value string) string {
	if strings.TrimSpace(value) == "" {
		return "(empty)"
	}
	runes := []rune(value)
	count := min(len(runes), 12)
	return strings.Repeat("*", count)
}

func emptySettingsValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "(empty)"
	}
	return value
}

func settingsSecretToggleButton(active bool) string {
	button := "[👁]"
	if active {
		return activeStyle.Render(button)
	}
	return mutedStyle.Render(button)
}

func boolSettingsValue(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func accessibleFormSettingsOptions() []settingsOption {
	return []settingsOption{
		{Label: "Enabled", Value: "true"},
		{Label: "Disabled", Value: "false"},
	}
}

func settingsSourceOptions() []settingsOption {
	catalog := app.SourceCatalog()
	options := make([]settingsOption, 0, len(catalog))
	for _, source := range catalog {
		options = append(options, settingsOption{Label: source.Label, Value: string(source.Source)})
	}
	return options
}

func cycleSettingsOption(options []settingsOption, value string, delta int) string {
	if len(options) == 0 {
		return value
	}
	current := 0
	for idx, option := range options {
		if option.Value == value {
			current = idx
			break
		}
	}
	next := (current + delta) % len(options)
	if next < 0 {
		next += len(options)
	}
	return options[next].Value
}

func dropLastRune(value string) string {
	if value == "" {
		return ""
	}
	runes := []rune(value)
	return string(runes[:len(runes)-1])
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

func settingsSourceEnabled(values []string, source string) bool {
	source = strings.ToLower(strings.TrimSpace(source))
	for _, value := range normalizeSettingsEnabledSources(values) {
		if value == source {
			return true
		}
	}
	return false
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
