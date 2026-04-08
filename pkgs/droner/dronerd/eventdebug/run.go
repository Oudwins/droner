package eventdebug

import (
	"context"
	"fmt"
	"strings"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/events/sessions/sessionslog"
	"github.com/Oudwins/droner/pkgs/droner/internals/env"
)

const defaultAddr = "localhost:57877"

type Config struct {
	Addr      string
	DBPath    string
	TableName string
	Title     string
}

func DefaultConfig() Config {
	return Config{
		Addr:      defaultAddr,
		DBPath:    sessionslog.DBPath(env.Get().DATA_DIR),
		TableName: defaultTableName,
		Title:     "Droner Event Debug",
	}
}

func Run(ctx context.Context, cfg Config) error {
	cfg = normalizeConfig(cfg)

	store, err := OpenSQLite(cfg.DBPath, SQLiteStoreOptions{TableName: cfg.TableName})
	if err != nil {
		return fmt.Errorf("open event debug store: %w", err)
	}
	defer store.Close()

	if err := ListenAndServe(ctx, cfg.Addr, store, ServerOptions{Title: cfg.Title}); err != nil && err != context.Canceled {
		return fmt.Errorf("run event debug server: %w", err)
	}
	return nil
}

func normalizeConfig(cfg Config) Config {
	defaults := DefaultConfig()
	if strings.TrimSpace(cfg.Addr) == "" {
		cfg.Addr = defaults.Addr
	}
	if strings.TrimSpace(cfg.DBPath) == "" {
		cfg.DBPath = defaults.DBPath
	}
	if strings.TrimSpace(cfg.TableName) == "" {
		cfg.TableName = defaults.TableName
	}
	if strings.TrimSpace(cfg.Title) == "" {
		cfg.Title = defaults.Title
	}
	return cfg
}
