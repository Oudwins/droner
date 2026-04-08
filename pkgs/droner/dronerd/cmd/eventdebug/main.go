package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Oudwins/droner/pkgs/droner/dronerd/eventdebug"
)

func main() {
	cfg := eventdebug.DefaultConfig()
	flag.StringVar(&cfg.Addr, "addr", cfg.Addr, "listen address")
	flag.StringVar(&cfg.DBPath, "db", cfg.DBPath, "path to sqlite database")
	flag.StringVar(&cfg.TableName, "table", cfg.TableName, "event log table name")
	flag.StringVar(&cfg.Title, "title", cfg.Title, "page title")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.SetOutput(os.Stdout)
	log.Printf("event debug listening on http://%s (db=%s, table=%s)", cfg.Addr, cfg.DBPath, cfg.TableName)
	if err := eventdebug.Run(ctx, cfg); err != nil {
		log.Fatal(fmt.Errorf("event debug failed: %w", err))
	}
}
