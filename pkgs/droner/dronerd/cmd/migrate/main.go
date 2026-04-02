package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	coredb "github.com/Oudwins/droner/pkgs/droner/dronerd/db"
	"github.com/Oudwins/droner/pkgs/droner/dronerd/sessionslog"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
	sqliteeventlog "github.com/Oudwins/droner/pkgs/droner/internals/eventlog/backends/sqlite"
	"github.com/pressly/goose/v3"
)

type migrationTarget struct {
	name        string
	path        string
	open        func(string) (*sql.DB, error)
	newProvider func(*sql.DB) (*goose.Provider, error)
}

func main() {
	ctx := context.Background()
	cfg := conf.GetConfig()

	fs := flag.NewFlagSet("dronerd-migrate", flag.ExitOnError)
	target := fs.String("target", "all", "migration target: main, sessionslog, or all")
	dataDir := fs.String("data-dir", cfg.Server.DataDir, "droner data directory")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: %s [--target main|sessionslog|all] [--data-dir path] <up|down|status|version>\n", os.Args[0])
		fs.PrintDefaults()
	}
	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(2)
	}
	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(2)
	}

	command := strings.ToLower(strings.TrimSpace(fs.Arg(0)))
	if err := run(ctx, command, strings.ToLower(strings.TrimSpace(*target)), *dataDir); err != nil {
		fmt.Fprintf(os.Stderr, "migrate: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, command string, target string, dataDir string) error {
	targets, err := selectTargets(target, dataDir)
	if err != nil {
		return err
	}
	for i, item := range targets {
		if i > 0 {
			fmt.Println()
		}
		if err := runTarget(ctx, command, item); err != nil {
			return err
		}
	}
	return nil
}

func selectTargets(target string, dataDir string) ([]migrationTarget, error) {
	main := migrationTarget{
		name:        "main",
		path:        coredb.DBPath(dataDir),
		open:        coredb.OpenSQLiteDBWithoutMigrations,
		newProvider: coredb.NewMigrationProvider,
	}
	sessions := migrationTarget{
		name:        "sessionslog",
		path:        sessionslog.DBPath(dataDir),
		open:        sqliteeventlog.OpenDB,
		newProvider: sqliteeventlog.NewMigrationProvider,
	}
	switch target {
	case "main":
		return []migrationTarget{main}, nil
	case "sessionslog":
		return []migrationTarget{sessions}, nil
	case "all", "":
		return []migrationTarget{main, sessions}, nil
	default:
		return nil, fmt.Errorf("unknown target %q", target)
	}
}

func runTarget(ctx context.Context, command string, target migrationTarget) error {
	fmt.Printf("== %s (%s) ==\n", target.name, target.path)

	conn, err := target.open(target.path)
	if err != nil {
		return err
	}
	defer conn.Close()

	provider, err := target.newProvider(conn)
	if err != nil {
		return err
	}

	switch command {
	case "up":
		return runUp(ctx, provider)
	case "down":
		return runDown(ctx, provider)
	case "status":
		return runStatus(ctx, provider)
	case "version":
		return runVersion(ctx, provider)
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}

func runUp(ctx context.Context, provider *goose.Provider) error {
	current, target, err := versionInfo(ctx, provider)
	if err != nil {
		return err
	}
	if current == target {
		fmt.Printf("already current at version %d\n", current)
		return nil
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		fmt.Printf("already current at version %d\n", target)
		return nil
	}
	for _, result := range results {
		fmt.Printf("applied %d %s\n", result.Source.Version, sourceName(result.Source))
	}
	return nil
}

func runDown(ctx context.Context, provider *goose.Provider) error {
	result, err := provider.Down(ctx)
	if errors.Is(err, goose.ErrNoNextVersion) {
		fmt.Println("no migration to roll back")
		return nil
	}
	if err != nil {
		return err
	}
	fmt.Printf("rolled back %d %s\n", result.Source.Version, sourceName(result.Source))
	return nil
}

func runStatus(ctx context.Context, provider *goose.Provider) error {
	current, target, err := versionInfo(ctx, provider)
	if err != nil {
		return err
	}
	fmt.Printf("current=%d target=%d\n", current, target)
	statuses, err := provider.Status(ctx)
	if err != nil {
		return err
	}
	for _, status := range statuses {
		appliedAt := "-"
		if !status.AppliedAt.IsZero() {
			appliedAt = status.AppliedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
		}
		fmt.Printf("%5d  %-8s  %-32s  %s\n", status.Source.Version, status.State, sourceName(status.Source), appliedAt)
	}
	return nil
}

func runVersion(ctx context.Context, provider *goose.Provider) error {
	current, target, err := versionInfo(ctx, provider)
	if err != nil {
		return err
	}
	fmt.Printf("current=%d target=%d\n", current, target)
	return nil
}

func versionInfo(ctx context.Context, provider *goose.Provider) (current int64, target int64, err error) {
	current, err = provider.GetDBVersion(ctx)
	if err != nil {
		return 0, 0, err
	}
	sources := provider.ListSources()
	if len(sources) == 0 {
		return current, 0, nil
	}
	return current, sources[len(sources)-1].Version, nil
}

func sourceName(source *goose.Source) string {
	if source == nil || source.Path == "" {
		return ""
	}
	return filepath.Base(source.Path)
}
