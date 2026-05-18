package config

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestLoadCreatesDefaultConfig(t *testing.T) {
	home := setupHome(t)

	cfg, err := Load(Options{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.ConfigCreated {
		t.Fatal("expected first load to create config")
	}
	if cfg.HTTPTimeout != defaultHTTPTimeout || cfg.HTTPCacheTTLHours != defaultHTTPCacheTTLHours || cfg.Theme != defaultTheme || cfg.GlamourStyle != defaultGlamourStyle || cfg.MarkdownImagePreview != defaultMarkdownImagePreview || cfg.AccessibleForms {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if !slices.Equal(cfg.EnabledSources, defaultEnabledSources()) {
		t.Fatalf("enabled sources = %#v, want defaults", cfg.EnabledSources)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "tildewire", "config.toml")); err != nil {
		t.Fatalf("default config was not written: %v", err)
	}
}

func TestLoadReadsConfigFile(t *testing.T) {
	setupHome(t)
	path := filepath.Join(t.TempDir(), "custom.toml")
	if err := os.WriteFile(path, []byte("http_timeout_seconds = 21\nhttp_cache_ttl_hours = 9\ntheme = \"dracula\"\nglamour_style = \"light\"\naccessible_forms = true\ndebug = true\nenabled_sources = [\"github\", \"producthunt\"]\ngithub_token = \"gh-file\"\nproduct_hunt_token = \"ph-file\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConfigCreated {
		t.Fatal("existing config should not be marked created")
	}
	if cfg.HTTPTimeout != 21 || cfg.HTTPCacheTTLHours != 9 || cfg.Theme != "dracula" || cfg.GlamourStyle != "light" || !cfg.AccessibleForms || !cfg.Debug {
		t.Fatalf("config values not loaded: %+v", cfg)
	}
	if !slices.Equal(cfg.EnabledSources, []string{"github", "producthunt"}) {
		t.Fatalf("enabled sources = %#v", cfg.EnabledSources)
	}
	if cfg.GitHubToken != "gh-file" || cfg.ProductHuntToken != "ph-file" {
		t.Fatalf("tokens not loaded from config: github=%q producthunt=%q", cfg.GitHubToken, cfg.ProductHuntToken)
	}
}

func TestLoadAndSaveSourceOrder(t *testing.T) {
	setupHome(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("source_order = [\"producthunt\", \"github\", \"hackernews\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.SourceOrder; !slices.Equal(got, []string{"producthunt", "github", "hackernews"}) {
		t.Fatalf("source order = %#v", got)
	}

	cfg.SourceOrder = []string{"hackernews", "github"}
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "source_order = [\"hackernews\", \"github\"]") {
		t.Fatalf("saved config missing source order:\n%s", data)
	}
}

func TestLoadAndSaveEnabledSources(t *testing.T) {
	setupHome(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("enabled_sources = [\"producthunt\", \"github\", \"github\", \"unknown\", \"\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.EnabledSources; !slices.Equal(got, []string{"producthunt", "github"}) {
		t.Fatalf("enabled sources = %#v", got)
	}

	cfg.EnabledSources = []string{"hackernews", "github"}
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "enabled_sources = [\"hackernews\", \"github\"]") {
		t.Fatalf("saved config missing enabled sources:\n%s", data)
	}
}

func TestLoadUpgradesLegacyDefaultEnabledSources(t *testing.T) {
	setupHome(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("enabled_sources = [\"github\", \"hackernews\", \"huggingface\", \"lobsters\", \"producthunt\"]\nsource_order = [\"github\", \"hackernews\", \"huggingface\", \"lobsters\", \"producthunt\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.EnabledSources; !slices.Equal(got, defaultEnabledSources()) {
		t.Fatalf("enabled sources = %#v, want upgraded defaults %#v", got, defaultEnabledSources())
	}
}

func TestLoadEnvOverridesConfigAndCLIOverridesEnv(t *testing.T) {
	setupHome(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("http_timeout_seconds = 10\ntheme = \"gruvbox\"\nglamour_style = \"light\"\naccessible_forms = false\ndebug = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TILDEWIRE_HTTP_TIMEOUT", "33")
	t.Setenv("TILDEWIRE_HTTP_CACHE_TTL_HOURS", "12")
	t.Setenv("TILDEWIRE_THEME", "nord")
	t.Setenv("TILDEWIRE_GLAMOUR_STYLE", "notty")
	t.Setenv("TILDEWIRE_ACCESSIBLE_FORMS", "true")
	t.Setenv("TILDEWIRE_DEBUG", "true")

	cfg, err := Load(Options{ConfigPath: path, Debug: false, DebugSet: true})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPTimeout != 33 || cfg.HTTPCacheTTLHours != 12 || cfg.Theme != "nord" || cfg.GlamourStyle != "notty" || !cfg.AccessibleForms {
		t.Fatalf("env overrides not applied: %+v", cfg)
	}
	if cfg.Debug {
		t.Fatalf("explicit CLI debug=false should override env debug=true: %+v", cfg)
	}
}

func TestLoadReadsProductHuntTokenFromConfigAndEnvOverrides(t *testing.T) {
	setupHome(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("product_hunt_token = \"from-file\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProductHuntToken != "from-file" {
		t.Fatalf("product hunt token = %q, want config value", cfg.ProductHuntToken)
	}

	t.Setenv("PRODUCT_HUNT_TOKEN", "from-env")
	cfg, err = Load(Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProductHuntToken != "from-env" {
		t.Fatalf("product hunt token = %q, want env value", cfg.ProductHuntToken)
	}

	t.Setenv("PRODUCT_HUNT_TOKEN", "")
	t.Setenv("TILDEWIRE_PRODUCT_HUNT_TOKEN", "prefixed-env")
	cfg, err = Load(Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProductHuntToken != "from-file" {
		t.Fatalf("product hunt token = %q, want config value when only prefixed env is set", cfg.ProductHuntToken)
	}

	cfg.ProductHuntToken = "persist-me"
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "product_hunt_token = \"persist-me\"") {
		t.Fatalf("product hunt token was not persisted:\n%s", data)
	}
}

func TestLoadReadsGitHubTokenFromConfigAndEnvOverrides(t *testing.T) {
	setupHome(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("github_token = \"from-file\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GitHubToken != "from-file" {
		t.Fatalf("github token = %q, want config value", cfg.GitHubToken)
	}

	t.Setenv("GITHUB_TOKEN", "from-env")
	cfg, err = Load(Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GitHubToken != "from-env" {
		t.Fatalf("github token = %q, want env value", cfg.GitHubToken)
	}

	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("TILDEWIRE_GITHUB_TOKEN", "prefixed-env")
	cfg, err = Load(Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GitHubToken != "from-file" {
		t.Fatalf("github token = %q, want config value when only prefixed env is set", cfg.GitHubToken)
	}

	cfg.GitHubToken = "persist-me"
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "github_token = \"persist-me\"") {
		t.Fatalf("github token was not persisted:\n%s", data)
	}
}

func TestLoadReadsMarkdownImagePreviewFromConfigAndEnvOverrides(t *testing.T) {
	setupHome(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("markdown_image_preview = \"halfblocks\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MarkdownImagePreview != "halfblocks" {
		t.Fatalf("markdown image preview = %q, want config value", cfg.MarkdownImagePreview)
	}

	t.Setenv("TILDEWIRE_MARKDOWN_IMAGE_PREVIEW", "off")
	cfg, err = Load(Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MarkdownImagePreview != "off" {
		t.Fatalf("markdown image preview = %q, want env value", cfg.MarkdownImagePreview)
	}

	cfg.MarkdownImagePreview = "iterm"
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "markdown_image_preview = \"iterm\"") {
		t.Fatalf("markdown image preview was not persisted:\n%s", data)
	}
}

func TestLoadAndSaveTheme(t *testing.T) {
	setupHome(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("theme = \"Tokyo Night\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != "tokyo-night" {
		t.Fatalf("theme = %q, want tokyo-night", cfg.Theme)
	}

	cfg.Theme = "rose-pine"
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "theme = \"rose-pine\"") {
		t.Fatalf("theme was not persisted:\n%s", data)
	}
}

func TestLoadRejectsInvalidThemeEnv(t *testing.T) {
	setupHome(t)
	t.Setenv("TILDEWIRE_THEME", "unknown-theme")

	_, err := Load(Options{})
	if err == nil || !strings.Contains(err.Error(), "TILDEWIRE_THEME") {
		t.Fatalf("expected theme error, got %v", err)
	}
}

func TestLoadNormalizesLegacyChafaMarkdownImagePreviewToHalfblocks(t *testing.T) {
	setupHome(t)
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("markdown_image_preview = \"chafa\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(Options{ConfigPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MarkdownImagePreview != "halfblocks" {
		t.Fatalf("markdown image preview = %q, want halfblocks", cfg.MarkdownImagePreview)
	}
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "markdown_image_preview = \"halfblocks\"") {
		t.Fatalf("legacy chafa was not normalized on save:\n%s", data)
	}
}

func TestLoadUsesEnvConfigPathUnlessCLIPathIsSet(t *testing.T) {
	setupHome(t)
	envPath := filepath.Join(t.TempDir(), "env.toml")
	cliPath := filepath.Join(t.TempDir(), "cli.toml")
	if err := os.WriteFile(envPath, []byte("glamour_style = \"env\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cliPath, []byte("glamour_style = \"cli\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TILDEWIRE_CONFIG", envPath)

	cfg, err := Load(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConfigPath != envPath || cfg.GlamourStyle != "env" {
		t.Fatalf("env config path not used: %+v", cfg)
	}

	cfg, err = Load(Options{ConfigPath: cliPath})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ConfigPath != cliPath || cfg.GlamourStyle != "cli" {
		t.Fatalf("cli config path should win: %+v", cfg)
	}
}

func TestLoadRejectsInvalidHTTPTimeout(t *testing.T) {
	setupHome(t)
	t.Setenv("TILDEWIRE_HTTP_TIMEOUT", "0")

	_, err := Load(Options{})
	if err == nil || !strings.Contains(err.Error(), "TILDEWIRE_HTTP_TIMEOUT") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestLoadRejectsInvalidHTTPCacheTTL(t *testing.T) {
	setupHome(t)
	t.Setenv("TILDEWIRE_HTTP_CACHE_TTL_HOURS", "0")

	_, err := Load(Options{})
	if err == nil || !strings.Contains(err.Error(), "TILDEWIRE_HTTP_CACHE_TTL_HOURS") {
		t.Fatalf("expected cache ttl error, got %v", err)
	}
}

func setupHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, key := range []string{
		"TILDEWIRE_CONFIG",
		"TILDEWIRE_HTTP_TIMEOUT",
		"TILDEWIRE_HTTP_CACHE_TTL_HOURS",
		"TILDEWIRE_THEME",
		"TILDEWIRE_GLAMOUR_STYLE",
		"TILDEWIRE_MARKDOWN_IMAGE_PREVIEW",
		"TILDEWIRE_ACCESSIBLE_FORMS",
		"TILDEWIRE_DEBUG",
		"GITHUB_TOKEN",
		"PRODUCT_HUNT_TOKEN",
		"TILDEWIRE_GITHUB_TOKEN",
		"TILDEWIRE_PRODUCT_HUNT_TOKEN",
	} {
		t.Setenv(key, "")
	}
	if runtime.GOOS == "windows" {
		t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
		t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	}
	return home
}
