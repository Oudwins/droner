package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os/signal"
	"syscall"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/events/sessions/sessionslog"
	"github.com/Oudwins/droner/pkgs/droner/internals/conf"
)

func main() {
	defaultDBPath := sessionslog.DBPath(conf.GetConfig().Server.DataDir)

	addr := flag.String("addr", "localhost:57877", "listen address")
	dbPath := flag.String("db", defaultDBPath, "path to sqlite database")
	tableName := flag.String("table", "event_log", "event log table name")
	title := flag.String("title", "Droner Event Debug", "page title")
	flag.Parse()

	store, err := OpenSQLite(*dbPath, SQLiteStoreOptions{TableName: *tableName})
	if err != nil {
		log.Fatal(fmt.Errorf("open event debug store: %w", err))
	}
	defer store.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("event debug listening on http://%s (db=%s, table=%s)", *addr, *dbPath, *tableName)
	if err := ListenAndServe(ctx, *addr, store, ServerOptions{Title: *title}); err != nil && err != context.Canceled {
		log.Fatal(fmt.Errorf("run event debug server: %w", err))
	}
}
