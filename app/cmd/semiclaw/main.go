package main

import (
	"context"
	"log"
	"os"
	"time"

	"semiclaw/app/internal/agent"
	"semiclaw/app/internal/auth"
	"semiclaw/app/internal/cli"
	"semiclaw/app/internal/config"
	"semiclaw/app/internal/db"
	"semiclaw/app/internal/filecmd"
	"semiclaw/app/internal/hostcmd"
	"semiclaw/app/internal/memorymd"
	"semiclaw/app/internal/pythoncmd"
	"semiclaw/app/internal/webcrawl"
)

var version = "dev"

func main() {
	ctx := context.Background()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to open database: %v", err)
	}
	defer func() { _ = database.Close() }()

	if err := db.RunMigrations(database, cfg.MigrationsDir); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	store := db.NewStore(database)
	secretBox, err := auth.LoadOrCreateSecretBox(cfg.EncryptionKeyPath)
	if err != nil {
		log.Fatalf("Failed to initialize secret box: %v", err)
	}

	agentService := agent.NewService(store)
	crawler := webcrawl.NewFetcher(20 * time.Second)
	hostCommandRunner := hostcmd.NewRunner(20*time.Second, 64*1024)
	pythonRunner := pythoncmd.NewRunner(20*time.Second, 64*1024)
	fileRunner := filecmd.NewRunner(".", 16*1024)
	memoryStore := memorymd.NewStore(cfg.DataDir)
	if err := memoryStore.Ensure(); err != nil {
		log.Fatalf("Failed to initialize markdown memory: %v", err)
	}
	scheduler := memorymd.NewScheduler(memoryStore, 30*time.Second, func(ctx context.Context, job memorymd.AutomationJob) error {
		return nil
	})
	go scheduler.Start(context.Background())

	runner := cli.NewRunner(cfg, store, secretBox, agentService, crawler, hostCommandRunner, pythonRunner, fileRunner, memoryStore, version, os.Stdin, os.Stdout, os.Stderr)

	if err := runner.Run(ctx, os.Args[1:]); err != nil {
		log.Fatalf("%v", err)
	}
}
