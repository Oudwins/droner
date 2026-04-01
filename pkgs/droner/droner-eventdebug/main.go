package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/eventdebug"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
)

func main() {
	defaultDBPath := filepath.Join(conf.GetConfig().Server.DataDir, "db", "droner.new.db")

	addr := flag.String("addr", "localhost:57877", "listen address")
	dbPath := flag.String("db", defaultDBPath, "path to sqlite database")
	tableName := flag.String("table", "event_log", "event log table name")
	title := flag.String("title", "Droner Event Debug", "page title")
	flag.Parse()

	store, err := eventdebug.OpenSQLite(*dbPath, eventdebug.SQLiteStoreOptions{TableName: *tableName})
	if err != nil {
		log.Fatal(fmt.Errorf("open event debug store: %w", err))
	}
	defer store.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("event debug listening on http://%s (db=%s, table=%s)", *addr, *dbPath, *tableName)
	if err := eventdebug.ListenAndServe(ctx, *addr, store, eventdebug.ServerOptions{Title: *title}); err != nil && err != context.Canceled {
		log.Fatal(fmt.Errorf("run event debug server: %w", err))
	}
}
