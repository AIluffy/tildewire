package tui

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"

	"github.com/AIluffy/tildewire/internal/domain"
)

type ruleDraft struct {
	Effect  string
	Target  string
	Value   string
	Enabled bool
}

func (m *Model) openRulesPanel() {
	m.openOverlay(overlayRules)
	m.ruleCursor = clamp(m.ruleCursor, 0, max(0, len(m.rules)-1))
	m.message = "personalization rules"
}

func (m *Model) openDedupePanel() {
	m.openOverlay(overlayDedupe)
	m.dedupeCursor = clamp(m.dedupeCursor, 0, max(0, len(m.dedupeCandidates)-1))
	m.message = "dedupe candidates"
}

func (m *Model) openRecommendDiagnosticsPanel() {
	m.openOverlay(overlayRecommendDiagnostics)
	m.recommendDiagnosticsLoading = true
	m.recommendDiagnostics = domain.RecommendationDiagnostics{}
	m.recommendDiagnosticsError = ""
	m.message = "recommend diagnostics"
}

func (m Model) handleRulesKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if next, cmd, ok := m.handleCommonKey(msg, closeOverlayBack); ok {
		return next, cmd
	}
	switch msg.String() {
	case "a":
		m.openRuleForm(0, domain.PersonalizationRule{})
		return m, m.ruleForm.Init()
	case "e":
		if rule, ok := m.selectedRule(); ok {
			m.openRuleForm(rule.ID, rule)
			return m, m.ruleForm.Init()
		}
	case "j", "down":
		m.ruleCursor = clamp(m.ruleCursor+1, 0, max(0, len(m.rules)-1))
	case "k", "up":
		m.ruleCursor = clamp(m.ruleCursor-1, 0, max(0, len(m.rules)-1))
	case " ", "space":
		if rule, ok := m.selectedRule(); ok {
			return m, m.setRuleEnabledCmd(rule.ID, !rule.Enabled)
		}
	case "d":
		if rule, ok := m.selectedRule(); ok {
			return m, m.deleteRuleCmd(rule.ID)
		}
	}
	return m, nil
}

func (m Model) handleDedupeKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if next, cmd, ok := m.handleCommonKey(msg, closeOverlayBack); ok {
		return next, cmd
	}
	switch msg.String() {
	case "j", "down":
		m.dedupeCursor = clamp(m.dedupeCursor+1, 0, max(0, len(m.dedupeCandidates)-1))
	case "k", "up":
		m.dedupeCursor = clamp(m.dedupeCursor-1, 0, max(0, len(m.dedupeCandidates)-1))
	case "i":
		if candidate, ok := m.selectedDedupeCandidate(); ok {
			return m, m.ignoreDedupeCandidateCmd(candidate.Key)
		}
	}
	return m, nil
}

func (m Model) handleRuleFormKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if key.Matches(msg, m.keys.Back) {
		m.closeOverlay()
		m.message = "rule cancelled"
		return m, nil
	}
	return m.updateRuleForm(msg)
}

func (m Model) updateRuleForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	updated, cmd := m.ruleForm.Update(msg)
	if form, ok := updated.(*huh.Form); ok {
		m.ruleForm = form
	}
	return m.applyRuleFormState(cmd)
}

func (m Model) applyRuleFormState(cmd tea.Cmd) (tea.Model, tea.Cmd) {
	switch m.ruleForm.State {
	case huh.StateCompleted:
		rule, err := m.ruleDraft.rule()
		if err != nil {
			m.message = "rule invalid"
			return m, cmd
		}
		id := m.ruleEditingID
		m.closeOverlay()
		if id > 0 {
			return m, tea.Batch(cmd, m.updateRuleCmd(id, rule))
		}
		return m, tea.Batch(cmd, m.createRuleCmd(rule))
	case huh.StateAborted:
		m.closeOverlay()
		m.message = "rule cancelled"
		return m, cmd
	default:
		return m, cmd
	}
}

func (m *Model) openRuleForm(id int64, rule domain.PersonalizationRule) {
	m.ruleEditingID = id
	m.ruleDraft = draftForRule(rule)
	title := "Add personalization rule"
	if id > 0 {
		title = "Edit personalization rule"
	}
	m.openOverlay(overlayRuleForm)
	m.message = strings.ToLower(title)
	m.ruleForm = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Key("rule_effect").
				Title("Effect").
				Options(
					huh.NewOption("Boost", string(domain.RuleEffectBoost)),
					huh.NewOption("Mute", string(domain.RuleEffectMute)),
					huh.NewOption("Hide", string(domain.RuleEffectHide)),
				).
				Value(&m.ruleDraft.Effect),
			huh.NewSelect[string]().
				Key("rule_target").
				Title("Target").
				Options(ruleTargetOptions()...).
				Value(&m.ruleDraft.Target),
			huh.NewInput().
				Key("rule_value").
				Title("Value").
				Value(&m.ruleDraft.Value).
				Validate(func(value string) error {
					if strings.TrimSpace(value) == "" {
						return fmt.Errorf("value is required")
					}
					return nil
				}),
			huh.NewConfirm().
				Key("rule_enabled").
				Title("Enabled").
				Affirmative("Enabled").
				Negative("Disabled").
				Value(&m.ruleDraft.Enabled),
		).Title(title),
	).
		WithAccessible(m.config.AccessibleForms).
		WithWidth(max(40, m.width-4)).
		WithHeight(max(8, m.height-4))
}

func (m Model) createRuleCmd(rule domain.PersonalizationRule) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		snapshot, err := m.service.CreatePersonalizationRule(ctx, rule, m.view, m.filter)
		return snapshotMsg{snapshot: snapshot, err: err, message: "rule created"}
	}
}

func (m Model) updateRuleCmd(id int64, rule domain.PersonalizationRule) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		snapshot, err := m.service.UpdatePersonalizationRule(ctx, id, rule, m.view, m.filter)
		return snapshotMsg{snapshot: snapshot, err: err, message: "rule updated"}
	}
}

func (m Model) setRuleEnabledCmd(id int64, enabled bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		snapshot, err := m.service.SetPersonalizationRuleEnabled(ctx, id, enabled, m.view, m.filter)
		message := "rule disabled"
		if enabled {
			message = "rule enabled"
		}
		return snapshotMsg{snapshot: snapshot, err: err, message: message}
	}
}

func (m Model) deleteRuleCmd(id int64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		snapshot, err := m.service.DeletePersonalizationRule(ctx, id, m.view, m.filter)
		return snapshotMsg{snapshot: snapshot, err: err, message: "rule deleted"}
	}
}

func (m Model) ignoreDedupeCandidateCmd(key string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		snapshot, err := m.service.IgnoreDedupeCandidate(ctx, key, m.view, m.filter)
		return snapshotMsg{snapshot: snapshot, err: err, message: "dedupe candidate ignored"}
	}
}

func (m Model) selectedRule() (domain.PersonalizationRule, bool) {
	if len(m.rules) == 0 {
		return domain.PersonalizationRule{}, false
	}
	return m.rules[clamp(m.ruleCursor, 0, len(m.rules)-1)], true
}

func (m Model) selectedDedupeCandidate() (domain.DedupeCandidate, bool) {
	if len(m.dedupeCandidates) == 0 {
		return domain.DedupeCandidate{}, false
	}
	return m.dedupeCandidates[clamp(m.dedupeCursor, 0, len(m.dedupeCandidates)-1)], true
}

func (m Model) ruleFromCurrentItem(effect domain.RuleEffect) (domain.PersonalizationRule, bool) {
	if search := strings.TrimSpace(m.filter.Search); search != "" {
		return newEnabledRule(effect, domain.RuleTargetKeyword, search), true
	}
	entry, ok := m.selected()
	if !ok {
		return domain.PersonalizationRule{}, false
	}
	item := entry.Item
	switch {
	case strings.TrimSpace(item.Refs.Repo) != "":
		return newEnabledRule(effect, domain.RuleTargetRepo, item.Refs.Repo), true
	case strings.TrimSpace(item.Language) != "":
		return newEnabledRule(effect, domain.RuleTargetLanguage, item.Language), true
	case len(item.Tags) > 0 && strings.TrimSpace(item.Tags[0]) != "":
		return newEnabledRule(effect, domain.RuleTargetTag, item.Tags[0]), true
	case strings.TrimSpace(item.Author) != "":
		return newEnabledRule(effect, domain.RuleTargetAuthor, item.Author), true
	case len(entry.Sources) > 0:
		return newEnabledRule(effect, domain.RuleTargetSource, string(entry.Sources[0].Source)), true
	default:
		keyword := firstKeyword(item.Title)
		if keyword == "" {
			return domain.PersonalizationRule{}, false
		}
		return newEnabledRule(effect, domain.RuleTargetKeyword, keyword), true
	}
}

func newEnabledRule(effect domain.RuleEffect, target domain.RuleTarget, value string) domain.PersonalizationRule {
	return domain.PersonalizationRule{Effect: effect, Target: target, Value: value, Enabled: true}
}

func draftForRule(rule domain.PersonalizationRule) ruleDraft {
	if rule.Effect == "" {
		rule.Effect = domain.RuleEffectBoost
	}
	if rule.Target == "" {
		rule.Target = domain.RuleTargetKeyword
	}
	if rule.ID == 0 && !rule.Enabled {
		rule.Enabled = true
	}
	return ruleDraft{
		Effect:  string(rule.Effect),
		Target:  string(rule.Target),
		Value:   rule.Value,
		Enabled: rule.Enabled,
	}
}

func (d ruleDraft) rule() (domain.PersonalizationRule, error) {
	value := strings.TrimSpace(d.Value)
	if value == "" {
		return domain.PersonalizationRule{}, fmt.Errorf("value is required")
	}
	return domain.PersonalizationRule{
		Effect:  domain.RuleEffect(strings.TrimSpace(d.Effect)),
		Target:  domain.RuleTarget(strings.TrimSpace(d.Target)),
		Value:   value,
		Enabled: d.Enabled,
	}, nil
}

func ruleTargetOptions() []huh.Option[string] {
	return []huh.Option[string]{
		huh.NewOption("Keyword", string(domain.RuleTargetKeyword)),
		huh.NewOption("Language", string(domain.RuleTargetLanguage)),
		huh.NewOption("Tag", string(domain.RuleTargetTag)),
		huh.NewOption("Domain", string(domain.RuleTargetDomain)),
		huh.NewOption("Source", string(domain.RuleTargetSource)),
		huh.NewOption("Repo", string(domain.RuleTargetRepo)),
		huh.NewOption("Author", string(domain.RuleTargetAuthor)),
	}
}

func firstKeyword(value string) string {
	var b strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			continue
		}
		if b.Len() > 0 {
			break
		}
	}
	return strings.ToLower(b.String())
}
