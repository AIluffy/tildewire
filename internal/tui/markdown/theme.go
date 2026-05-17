package markdown

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

func themedColor(value string, fallback color.Color) color.Color {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return lipgloss.Color(value)
}

func (theme Theme) accentColor() color.Color {
	return themedColor(theme.Accent, markdownTableBorderColor)
}

func (theme Theme) mutedColor() color.Color {
	return themedColor(theme.Muted, lipgloss.Color("#A1A1AA"))
}

func (theme Theme) textColor() color.Color {
	return themedColor(theme.Text, lipgloss.Color("#D4D4D8"))
}

func (theme Theme) codeTextColor(fallback string) color.Color {
	return themedColor(theme.CodeText, lipgloss.Color(fallback))
}
