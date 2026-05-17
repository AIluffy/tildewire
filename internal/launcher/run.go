package launcher

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/AIluffy/tildewire/internal/app"
	"github.com/AIluffy/tildewire/internal/boot"
	"github.com/AIluffy/tildewire/internal/config"
	"github.com/AIluffy/tildewire/internal/domain"
	"github.com/AIluffy/tildewire/internal/httpx"
	"github.com/AIluffy/tildewire/internal/sources"
	"github.com/AIluffy/tildewire/internal/store"
	"github.com/AIluffy/tildewire/internal/tui"
)

// Run starts tildewire with the provided command-line display name.
func Run(programName, version string, args []string, output io.Writer) error {
	options, err := boot.ParseArgsFor(programName, args, output)
	if err != nil {
		return err
	}
	if options.ShowHelp {
		return nil
	}
	if options.ShowVersion {
		fmt.Fprintln(output, version)
		return nil
	}

	cfg, err := config.Load(config.Options{
		ConfigPath: options.ConfigPath,
		Debug:      options.Debug,
		DebugSet:   options.DebugSet,
		Version:    version,
	})
	if err != nil {
		return err
	}
	if err := config.EnsureDirs(cfg); err != nil {
		return err
	}
	if cfg.Debug {
		if err := enableFileLogging(cfg.StateDir); err != nil {
			return err
		}
	}

	ctx := context.Background()
	db, err := store.Open(ctx, cfg.Database)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		return err
	}

	httpClient := httpx.New(time.Duration(cfg.HTTPTimeout)*time.Second, db)
	httpClient.SetCacheTTL(cfg.HTTPCacheTTL())
	service := app.NewService(
		db,
		httpClient,
		[]app.SourceAdapter{
			sources.NewGitHubTrendingAdapter(cfg.GitHubToken),
			sources.NewHackerNewsAdapter(),
			sources.NewHuggingFacePapersAdapter(),
			sources.NewLobstersAdapter(),
			sources.NewProductHuntAdapter(cfg.ProductHuntToken),
		},
	)
	service.SetSourceConfig(configuredEnabledSources(cfg.EnabledSources), sourceTokens(cfg))
	initial, err := service.LoadFeed(ctx, domain.SourceAll, app.FeedFilter{})
	if err != nil {
		return err
	}

	program := tea.NewProgram(tui.NewModel(service, initial, tui.ModelOptions{
		Config:   cfg,
		FirstRun: cfg.ConfigCreated,
		SaveConfig: func(next config.Config) error {
			next.ConfigPath = cfg.ConfigPath
			next.DataDir = cfg.DataDir
			next.CacheDir = cfg.CacheDir
			next.StateDir = cfg.StateDir
			next.Database = cfg.Database
			next.Version = cfg.Version
			if err := config.Save(next); err != nil {
				return err
			}
			cfg = next
			return nil
		},
	}))
	_, err = program.Run()
	return err
}

func configuredEnabledSources(values []string) []domain.SourceID {
	sources := make([]domain.SourceID, 0, len(values))
	for _, value := range values {
		sources = append(sources, domain.SourceID(value))
	}
	return sources
}

func sourceTokens(cfg config.Config) map[domain.SourceID]string {
	return map[domain.SourceID]string{
		domain.SourceGitHub:      cfg.GitHubToken,
		domain.SourceProductHunt: cfg.ProductHuntToken,
	}
}

func enableFileLogging(stateDir string) error {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(stateDir, "debug.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	log.SetOutput(file)
	return nil
}
