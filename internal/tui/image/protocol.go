package image

import (
	"os"
	"strings"
	"sync"

	"github.com/blacktop/go-termimg"
	"golang.org/x/term"
)

type terminalProbe interface {
	Env(string) string
	IsStdoutTTY() bool
	LocaleIsUTF8() bool
	Protocols() []ImageProtocol
	Metrics() TerminalMetrics
}

type realTerminalProbe struct{}

var (
	defaultProbeMu sync.RWMutex
	defaultProbe   terminalProbe = realTerminalProbe{}
)

// DetectStableProtocol chooses the safest protocol for a render mode.
func DetectStableProtocol(config ImageManagerConfig, mode ImageRenderMode) ImageProtocol {
	defaultProbeMu.RLock()
	probe := defaultProbe
	defaultProbeMu.RUnlock()
	return detectStableProtocol(config.normalized(), normalizeRenderMode(mode), probe, false)
}

func detectStableProtocol(config ImageManagerConfig, mode ImageRenderMode, probe terminalProbe, sessionGraphicsDisabled bool) ImageProtocol {
	if sessionGraphicsDisabled || !config.EnableGraphicsProtocols {
		return ProtocolHalfblocks
	}
	if terminalForcesFallback(probe) {
		return ProtocolHalfblocks
	}

	inTmux := strings.TrimSpace(probe.Env("TMUX")) != "" || strings.EqualFold(probe.Env("TERM_PROGRAM"), "tmux")
	if mode == RenderModeScrollableInline {
		if !config.UnsafeInlineGraphicsInScrollableList || inTmux {
			return ProtocolHalfblocks
		}
	}
	if inTmux && !config.EnableTmuxPassthrough {
		return ProtocolHalfblocks
	}

	available := protocolSet(probe.Protocols())
	if len(available) == 0 {
		return ProtocolHalfblocks
	}

	preferred := normalizeProtocol(config.PreferredProtocol)
	if preferred != ProtocolAuto && preferred != ProtocolHalfblocks && available[preferred] {
		if protocolNeedsCellMetrics(preferred) && !usableMetrics(probe.Metrics()) {
			return ProtocolHalfblocks
		}
		return preferred
	}
	if preferred == ProtocolHalfblocks {
		return ProtocolHalfblocks
	}

	for _, protocol := range []ImageProtocol{ProtocolKitty, ProtocolITerm2, ProtocolSixel} {
		if available[protocol] {
			if protocolNeedsCellMetrics(protocol) && !usableMetrics(probe.Metrics()) {
				return ProtocolHalfblocks
			}
			return protocol
		}
	}
	return ProtocolHalfblocks
}

func terminalForcesFallback(probe terminalProbe) bool {
	if !probe.IsStdoutTTY() || !probe.LocaleIsUTF8() {
		return true
	}
	termName := strings.ToLower(strings.TrimSpace(probe.Env("TERM")))
	if termName == "dumb" {
		return true
	}
	if truthy(probe.Env("CI")) {
		return true
	}
	return false
}

func protocolSet(protocols []ImageProtocol) map[ImageProtocol]bool {
	set := make(map[ImageProtocol]bool, len(protocols)+1)
	for _, protocol := range protocols {
		set[normalizeProtocol(protocol)] = true
	}
	set[ProtocolHalfblocks] = true
	return set
}

func normalizeProtocol(protocol ImageProtocol) ImageProtocol {
	switch ImageProtocol(strings.ToLower(strings.TrimSpace(string(protocol)))) {
	case ProtocolKitty:
		return ProtocolKitty
	case ProtocolITerm2, "iterm":
		return ProtocolITerm2
	case ProtocolSixel:
		return ProtocolSixel
	case ProtocolHalfblocks:
		return ProtocolHalfblocks
	default:
		return ProtocolAuto
	}
}

func normalizeRenderMode(mode ImageRenderMode) ImageRenderMode {
	switch mode {
	case RenderModeScrollableInline:
		return RenderModeScrollableInline
	default:
		return RenderModePreviewPane
	}
}

func protocolNeedsCellMetrics(protocol ImageProtocol) bool {
	return protocol == ProtocolKitty || protocol == ProtocolITerm2 || protocol == ProtocolSixel
}

func usableMetrics(metrics TerminalMetrics) bool {
	return metrics.FontWidth > 0 && metrics.FontHeight > 0
}

func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func (realTerminalProbe) Env(key string) string {
	return os.Getenv(key)
}

func (realTerminalProbe) IsStdoutTTY() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

func (realTerminalProbe) LocaleIsUTF8() bool {
	locale := firstNonEmpty(os.Getenv("LC_ALL"), os.Getenv("LC_CTYPE"), os.Getenv("LANG"))
	if strings.TrimSpace(locale) == "" {
		return true
	}
	locale = strings.ToUpper(locale)
	return strings.Contains(locale, "UTF-8") || strings.Contains(locale, "UTF8")
}

func (realTerminalProbe) Protocols() []ImageProtocol {
	protocols := termimg.DetermineProtocols()
	result := make([]ImageProtocol, 0, len(protocols))
	for _, protocol := range protocols {
		if converted := fromTermimgProtocol(protocol); converted != ProtocolAuto {
			result = append(result, converted)
		}
	}
	if len(result) == 0 {
		result = append(result, ProtocolHalfblocks)
	}
	return result
}

func (realTerminalProbe) Metrics() TerminalMetrics {
	if os.Getenv("TMUX") != "" && os.Getenv("TERMIMG_BYPASS_DETECTION") == "" {
		return TerminalMetrics{FontWidth: 8, FontHeight: 16}
	}
	features := termimg.QueryTerminalFeatures()
	if features == nil {
		return TerminalMetrics{FontWidth: 8, FontHeight: 16}
	}
	metrics := TerminalMetrics{
		FontWidth:  features.FontWidth,
		FontHeight: features.FontHeight,
		Columns:    features.WindowCols,
		Rows:       features.WindowRows,
	}
	if metrics.FontWidth <= 0 {
		metrics.FontWidth = 8
	}
	if metrics.FontHeight <= 0 {
		metrics.FontHeight = 16
	}
	return metrics
}

func fromTermimgProtocol(protocol termimg.Protocol) ImageProtocol {
	switch protocol {
	case termimg.Kitty:
		return ProtocolKitty
	case termimg.ITerm2:
		return ProtocolITerm2
	case termimg.Sixel:
		return ProtocolSixel
	case termimg.Halfblocks:
		return ProtocolHalfblocks
	default:
		return ProtocolAuto
	}
}

func toTermimgProtocol(protocol ImageProtocol) termimg.Protocol {
	switch normalizeProtocol(protocol) {
	case ProtocolKitty:
		return termimg.Kitty
	case ProtocolITerm2:
		return termimg.ITerm2
	case ProtocolSixel:
		return termimg.Sixel
	case ProtocolHalfblocks:
		return termimg.Halfblocks
	default:
		return termimg.Auto
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
