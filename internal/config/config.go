package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const (
	defaultHTTPTimeout              = 12
	defaultHTTPCacheTTLHours        = 6
	defaultGlamourStyle             = "dark"
	defaultMarkdownImagePreview     = "auto"
	MarkdownImagePreviewAuto        = "auto"
	MarkdownImagePreviewOff         = "off"
	MarkdownImagePreviewKitty       = "kitty"
	MarkdownImagePreviewITerm       = "iterm"
	MarkdownImagePreviewSixel       = "sixel"
	MarkdownImagePreviewHalfblocks  = "halfblocks"
	markdownImagePreviewLegacyChafa = "chafa"
)

// Config contains resolved paths and startup switches.
type Config struct {
	ConfigPath           string
	DataDir              string
	CacheDir             string
	StateDir             string
	Database             string
	Debug                bool
	Version              string
	HTTPTimeout          int
	HTTPCacheTTLHours    int
	GlamourStyle         string
	MarkdownImagePreview string
	AccessibleForms      bool
	SourceOrder          []string
	EnabledSources       []string
	GitHubToken          string
	ProductHuntToken     string
	ConfigCreated        bool
}

// Options are CLI-level config overrides.
type Options struct {
	ConfigPath string
	Debug      bool
	DebugSet   bool
	Version    string
}

type fileConfig struct {
	HTTPTimeout          int      `toml:"http_timeout_seconds"`
	HTTPCacheTTLHours    int      `toml:"http_cache_ttl_hours"`
	GlamourStyle         string   `toml:"glamour_style"`
	MarkdownImagePreview string   `toml:"markdown_image_preview"`
	AccessibleForms      bool     `toml:"accessible_forms"`
	SourceOrder          []string `toml:"source_order,omitempty"`
	EnabledSources       []string `toml:"enabled_sources,omitempty"`
	GitHubToken          string   `toml:"github_token,omitempty"`
	ProductHuntToken     string   `toml:"product_hunt_token,omitempty"`
	Debug                bool     `toml:"debug"`
}

// Load resolves default tildewire paths and applies config, env, and CLI overrides.
func Load(options Options) (Config, error) {
	cfg, err := defaultConfig(options.Version)
	if err != nil {
		return Config{}, err
	}
	if envPath := strings.TrimSpace(os.Getenv("TILDEWIRE_CONFIG")); envPath != "" {
		cfg.ConfigPath = envPath
	}
	if strings.TrimSpace(options.ConfigPath) != "" {
		cfg.ConfigPath = options.ConfigPath
	}

	created, err := loadOrCreateFile(&cfg)
	if err != nil {
		return Config{}, err
	}
	cfg.ConfigCreated = created

	if err := applyEnv(&cfg); err != nil {
		return Config{}, err
	}
	if options.DebugSet {
		cfg.Debug = options.Debug
	}
	cfg.Database = filepath.Join(cfg.DataDir, "tildewire.db")
	return cfg, nil
}

// Save writes the user-editable configuration file.
func Save(cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(cfg.ConfigPath), 0o755); err != nil {
		return err
	}
	data := fileConfig{
		HTTPTimeout:          positiveOrDefault(cfg.HTTPTimeout, defaultHTTPTimeout),
		HTTPCacheTTLHours:    positiveOrDefault(cfg.HTTPCacheTTLHours, defaultHTTPCacheTTLHours),
		GlamourStyle:         nonEmptyOrDefault(cfg.GlamourStyle, defaultGlamourStyle),
		MarkdownImagePreview: normalizeMarkdownImagePreviewOrDefault(cfg.MarkdownImagePreview),
		AccessibleForms:      cfg.AccessibleForms,
		SourceOrder:          normalizeSourceOrderValues(cfg.SourceOrder),
		EnabledSources:       normalizeEnabledSourcesOrDefault(cfg.EnabledSources),
		GitHubToken:          strings.TrimSpace(cfg.GitHubToken),
		ProductHuntToken:     strings.TrimSpace(cfg.ProductHuntToken),
		Debug:                cfg.Debug,
	}
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(data); err != nil {
		return err
	}
	return os.WriteFile(cfg.ConfigPath, buf.Bytes(), 0o644)
}

// EnsureDirs creates the local directories required at startup.
func EnsureDirs(cfg Config) error {
	for _, dir := range []string{filepath.Dir(cfg.ConfigPath), cfg.DataDir, cfg.CacheDir, cfg.StateDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

func defaultConfig(version string) (Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		ConfigPath:           filepath.Join(home, ".config", "tildewire", "config.toml"),
		DataDir:              filepath.Join(home, ".local", "share", "tildewire"),
		CacheDir:             filepath.Join(home, ".cache", "tildewire"),
		StateDir:             filepath.Join(home, ".local", "state", "tildewire"),
		Version:              version,
		HTTPTimeout:          defaultHTTPTimeout,
		HTTPCacheTTLHours:    defaultHTTPCacheTTLHours,
		GlamourStyle:         defaultGlamourStyle,
		MarkdownImagePreview: defaultMarkdownImagePreview,
		EnabledSources:       defaultEnabledSources(),
	}
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		localAppData := os.Getenv("LOCALAPPDATA")
		if appData == "" || localAppData == "" {
			return Config{}, errors.New("APPDATA and LOCALAPPDATA must be set on Windows")
		}
		cfg.ConfigPath = filepath.Join(appData, "tildewire", "config.toml")
		cfg.DataDir = filepath.Join(localAppData, "tildewire")
		cfg.CacheDir = filepath.Join(localAppData, "tildewire", "cache")
		cfg.StateDir = filepath.Join(localAppData, "tildewire", "state")
	}
	cfg.Database = filepath.Join(cfg.DataDir, "tildewire.db")
	return cfg, nil
}

func loadOrCreateFile(cfg *Config) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.ConfigPath), 0o755); err != nil {
		return false, err
	}
	data, err := os.ReadFile(cfg.ConfigPath)
	if errors.Is(err, os.ErrNotExist) {
		if err := Save(*cfg); err != nil {
			return false, err
		}
		return true, nil
	}
	if err != nil {
		return false, err
	}
	var file fileConfig
	if _, err := toml.Decode(string(data), &file); err != nil {
		return false, fmt.Errorf("read config %s: %w", cfg.ConfigPath, err)
	}
	applyFile(cfg, file)
	return false, nil
}

func applyFile(cfg *Config, file fileConfig) {
	if file.HTTPTimeout > 0 {
		cfg.HTTPTimeout = file.HTTPTimeout
	}
	if file.HTTPCacheTTLHours > 0 {
		cfg.HTTPCacheTTLHours = file.HTTPCacheTTLHours
	}
	if strings.TrimSpace(file.GlamourStyle) != "" {
		cfg.GlamourStyle = strings.TrimSpace(file.GlamourStyle)
	}
	if strings.TrimSpace(file.MarkdownImagePreview) != "" {
		cfg.MarkdownImagePreview = normalizeMarkdownImagePreviewOrDefault(file.MarkdownImagePreview)
	}
	cfg.AccessibleForms = file.AccessibleForms
	cfg.SourceOrder = normalizeSourceOrderValues(file.SourceOrder)
	if enabled := normalizeEnabledSourceValues(file.EnabledSources); len(enabled) > 0 {
		cfg.EnabledSources = enabled
	}
	if token := strings.TrimSpace(file.GitHubToken); token != "" {
		cfg.GitHubToken = token
	}
	if token := strings.TrimSpace(file.ProductHuntToken); token != "" {
		cfg.ProductHuntToken = token
	}
	cfg.Debug = file.Debug
}

func applyEnv(cfg *Config) error {
	if value := strings.TrimSpace(os.Getenv("TILDEWIRE_HTTP_TIMEOUT")); value != "" {
		timeout, err := strconv.Atoi(value)
		if err != nil || timeout <= 0 {
			return fmt.Errorf("TILDEWIRE_HTTP_TIMEOUT must be a positive integer")
		}
		cfg.HTTPTimeout = timeout
	}
	if value := strings.TrimSpace(os.Getenv("TILDEWIRE_HTTP_CACHE_TTL_HOURS")); value != "" {
		ttlHours, err := strconv.Atoi(value)
		if err != nil || ttlHours <= 0 {
			return fmt.Errorf("TILDEWIRE_HTTP_CACHE_TTL_HOURS must be a positive integer")
		}
		cfg.HTTPCacheTTLHours = ttlHours
	}
	if value := strings.TrimSpace(os.Getenv("TILDEWIRE_GLAMOUR_STYLE")); value != "" {
		cfg.GlamourStyle = value
	}
	if value := strings.TrimSpace(os.Getenv("TILDEWIRE_MARKDOWN_IMAGE_PREVIEW")); value != "" {
		mode, ok := normalizeMarkdownImagePreview(value)
		if !ok {
			return fmt.Errorf("TILDEWIRE_MARKDOWN_IMAGE_PREVIEW must be one of auto, off, kitty, iterm, sixel, halfblocks")
		}
		cfg.MarkdownImagePreview = mode
	}
	if value := strings.TrimSpace(os.Getenv("TILDEWIRE_ACCESSIBLE_FORMS")); value != "" {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("TILDEWIRE_ACCESSIBLE_FORMS must be a boolean")
		}
		cfg.AccessibleForms = enabled
	}
	if value := strings.TrimSpace(os.Getenv("TILDEWIRE_DEBUG")); value != "" {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("TILDEWIRE_DEBUG must be a boolean")
		}
		cfg.Debug = enabled
	}
	if value := strings.TrimSpace(os.Getenv("GITHUB_TOKEN")); value != "" {
		cfg.GitHubToken = value
	}
	if value := strings.TrimSpace(os.Getenv("PRODUCT_HUNT_TOKEN")); value != "" {
		cfg.ProductHuntToken = value
	}
	return nil
}

// HTTPCacheTTL returns the configured raw HTTP cache lifetime.
func (cfg Config) HTTPCacheTTL() time.Duration {
	return time.Duration(positiveOrDefault(cfg.HTTPCacheTTLHours, defaultHTTPCacheTTLHours)) * time.Hour
}

func positiveOrDefault(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func nonEmptyOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func normalizeMarkdownImagePreviewOrDefault(value string) string {
	if mode, ok := normalizeMarkdownImagePreview(value); ok {
		return mode
	}
	return defaultMarkdownImagePreview
}

func normalizeMarkdownImagePreview(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case MarkdownImagePreviewAuto:
		return MarkdownImagePreviewAuto, true
	case MarkdownImagePreviewOff:
		return MarkdownImagePreviewOff, true
	case MarkdownImagePreviewKitty:
		return MarkdownImagePreviewKitty, true
	case MarkdownImagePreviewITerm:
		return MarkdownImagePreviewITerm, true
	case MarkdownImagePreviewSixel:
		return MarkdownImagePreviewSixel, true
	case MarkdownImagePreviewHalfblocks, markdownImagePreviewLegacyChafa:
		return MarkdownImagePreviewHalfblocks, true
	default:
		return "", false
	}
}

func normalizeSourceOrderValues(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		normalized = append(normalized, value)
	}
	return normalized
}

func defaultEnabledSources() []string {
	return []string{"github", "hackernews", "huggingface", "lobsters", "producthunt"}
}

func normalizeEnabledSourcesOrDefault(values []string) []string {
	normalized := normalizeEnabledSourceValues(values)
	if len(normalized) == 0 {
		return defaultEnabledSources()
	}
	return normalized
}

func normalizeEnabledSourceValues(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	allowed := make(map[string]bool)
	for _, source := range defaultEnabledSources() {
		allowed[source] = true
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
