package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/kong-jing/meowbot/internal/bot"
	"github.com/kong-jing/meowbot/internal/config"
	"github.com/kong-jing/meowbot/internal/db"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	store, err := db.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer store.Close()
	if err := store.Migrate(); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cmd := "bot"
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		cmd = os.Args[1]
	}
	switch cmd {
	case "bot", "serve":
		return bot.New(cfg, store).Run(ctx)
	case "status":
		fmt.Println(store.Status(ctx))
		return nil
	case "backup":
		path, err := store.Backup(ctx, cfg.BackupDir)
		if err != nil {
			return err
		}
		fmt.Println(path)
		return nil
	case "migrate":
		return nil
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `meowbot

Commands:
  bot       run Telegram secretary
  status    print database counts
  backup    write a compact SQLite backup
	migrate   create/update schema
`)
}
