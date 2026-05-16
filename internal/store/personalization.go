package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/zhangxueai/tildewire/internal/domain"
	"github.com/zhangxueai/tildewire/internal/store/generated"
)

// CreatePersonalizationRule persists a new enabled personalization rule.
func (s *Store) CreatePersonalizationRule(ctx context.Context, rule domain.PersonalizationRule) (domain.PersonalizationRule, error) {
	rule, err := normalizePersonalizationRule(rule)
	if err != nil {
		return domain.PersonalizationRule{}, err
	}
	now := time.Now().UTC()
	row, err := s.queries.CreatePersonalizationRule(ctx, generated.CreatePersonalizationRuleParams{
		Effect:    string(rule.Effect),
		Target:    string(rule.Target),
		Value:     rule.Value,
		Enabled:   boolInt(rule.Enabled),
		CreatedAt: formatTime(now),
		UpdatedAt: formatTime(now),
	})
	if err != nil {
		return domain.PersonalizationRule{}, err
	}
	return scanPersonalizationRule(row), nil
}

// UpdatePersonalizationRule replaces a durable personalization rule.
func (s *Store) UpdatePersonalizationRule(ctx context.Context, id int64, rule domain.PersonalizationRule) (domain.PersonalizationRule, error) {
	rule, err := normalizePersonalizationRule(rule)
	if err != nil {
		return domain.PersonalizationRule{}, err
	}
	row, err := s.queries.UpdatePersonalizationRule(ctx, generated.UpdatePersonalizationRuleParams{
		Effect:    string(rule.Effect),
		Target:    string(rule.Target),
		Value:     rule.Value,
		Enabled:   boolInt(rule.Enabled),
		UpdatedAt: formatTime(time.Now().UTC()),
		ID:        id,
	})
	if err != nil {
		return domain.PersonalizationRule{}, err
	}
	return scanPersonalizationRule(row), nil
}

// ListPersonalizationRules returns rules ordered for display and matching.
func (s *Store) ListPersonalizationRules(ctx context.Context, includeDisabled bool) ([]domain.PersonalizationRule, error) {
	rows, err := s.queries.ListPersonalizationRules(ctx, boolInt(includeDisabled))
	if err != nil {
		return nil, err
	}
	rules := make([]domain.PersonalizationRule, 0, len(rows))
	for _, row := range rows {
		rules = append(rules, scanPersonalizationRule(row))
	}
	return rules, nil
}

// SetPersonalizationRuleEnabled toggles one rule.
func (s *Store) SetPersonalizationRuleEnabled(ctx context.Context, id int64, enabled bool) error {
	return s.queries.SetPersonalizationRuleEnabled(ctx, generated.SetPersonalizationRuleEnabledParams{
		Enabled:   boolInt(enabled),
		UpdatedAt: formatTime(time.Now().UTC()),
		ID:        id,
	})
}

// DeletePersonalizationRule removes one rule.
func (s *Store) DeletePersonalizationRule(ctx context.Context, id int64) error {
	return s.queries.DeletePersonalizationRule(ctx, id)
}

// PreferenceProfile returns implicit saved and hidden preference signals.
func (s *Store) PreferenceProfile(ctx context.Context) (domain.PreferenceProfile, error) {
	rows, err := s.queries.ListPreferenceSignals(ctx)
	if err != nil {
		return domain.PreferenceProfile{}, err
	}
	profile := newPreferenceProfile()
	for _, row := range rows {
		addPreferenceSignal(&profile, row.State, row.Target, row.Value, int(row.Total))
	}
	return profile, nil
}

func normalizePersonalizationRule(rule domain.PersonalizationRule) (domain.PersonalizationRule, error) {
	rule.Effect = domain.RuleEffect(strings.ToLower(strings.TrimSpace(string(rule.Effect))))
	rule.Target = domain.RuleTarget(strings.ToLower(strings.TrimSpace(string(rule.Target))))
	rule.Value = strings.ToLower(strings.TrimSpace(rule.Value))
	if rule.Value == "" {
		return domain.PersonalizationRule{}, errors.New("personalization rule value is required")
	}
	switch rule.Effect {
	case domain.RuleEffectBoost, domain.RuleEffectMute, domain.RuleEffectHide:
	default:
		return domain.PersonalizationRule{}, errors.New("invalid personalization rule effect")
	}
	switch rule.Target {
	case domain.RuleTargetKeyword, domain.RuleTargetLanguage, domain.RuleTargetTag, domain.RuleTargetDomain, domain.RuleTargetSource, domain.RuleTargetRepo, domain.RuleTargetAuthor:
	default:
		return domain.PersonalizationRule{}, errors.New("invalid personalization rule target")
	}
	return rule, nil
}

func scanPersonalizationRule(row generated.PersonalizationRule) domain.PersonalizationRule {
	return domain.PersonalizationRule{
		ID:        row.ID,
		Effect:    domain.RuleEffect(row.Effect),
		Target:    domain.RuleTarget(row.Target),
		Value:     row.Value,
		Enabled:   row.Enabled == 1,
		CreatedAt: parseTime(row.CreatedAt),
		UpdatedAt: parseTime(row.UpdatedAt),
	}
}

func newPreferenceProfile() domain.PreferenceProfile {
	return domain.PreferenceProfile{
		SavedTags:       make(map[string]int),
		SavedLanguages:  make(map[string]int),
		SavedRepos:      make(map[string]int),
		SavedAuthors:    make(map[string]int),
		SavedSources:    make(map[string]int),
		HiddenTags:      make(map[string]int),
		HiddenLanguages: make(map[string]int),
		HiddenRepos:     make(map[string]int),
		HiddenAuthors:   make(map[string]int),
		HiddenSources:   make(map[string]int),
	}
}

func addPreferenceSignal(profile *domain.PreferenceProfile, state, target, value string, total int) {
	if total <= 0 {
		return
	}
	switch state + ":" + target {
	case "saved:tag":
		profile.SavedTags[value] = total
	case "saved:language":
		profile.SavedLanguages[value] = total
	case "saved:repo":
		profile.SavedRepos[value] = total
	case "saved:author":
		profile.SavedAuthors[value] = total
	case "saved:source":
		profile.SavedSources[value] = total
	case "hidden:tag":
		profile.HiddenTags[value] = total
	case "hidden:language":
		profile.HiddenLanguages[value] = total
	case "hidden:repo":
		profile.HiddenRepos[value] = total
	case "hidden:author":
		profile.HiddenAuthors[value] = total
	case "hidden:source":
		profile.HiddenSources[value] = total
	}
}
