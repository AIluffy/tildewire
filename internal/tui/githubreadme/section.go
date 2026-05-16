package githubreadme

import (
	"strings"

	"github.com/zhangxueai/tildewire/internal/domain"
)

// Section returns the GitHub README detail section, when present.
func Section(detail domain.ItemDetail) (domain.DetailSection, bool) {
	for _, section := range detail.Sections {
		if strings.TrimSpace(section.Body) == "" {
			continue
		}
		if section.Source == domain.SourceGitHub || strings.EqualFold(strings.TrimSpace(section.Title), "GitHub README") {
			return section, true
		}
	}
	return domain.DetailSection{}, false
}
