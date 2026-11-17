// Command paperboat is the fish rental application. One binary holds every service; the
// first argument selects one of api, worker, migrate, loadgen or web.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/api"
	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/buildinfo"
	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/httplog"
	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/loadgen"
	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/store"
	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/web"
	"github.com/statik/site-reliability-clanker/apps/paperboat/internal/worker"
)

const (
	usage = "usage: paperboat <api|worker|migrate|loadgen|web>"
	// databaseWait is how long a service waits for Postgres at start.
	databaseWait = 60 * time.Second
)

type command func(ctx context.Context, build buildinfo.Info, logger *slog.Logger) error

func main() {
	commands := map[string]command{
		"api":     runAPI,
		"worker":  runWorker,
		"migrate": runMigrate,
		"loadgen": runLoadgen,
		"web":     runWeb,
	}
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(2)
	}
	run, ok := commands[os.Args[1]]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n%s\n", os.Args[1], usage)
		os.Exit(2)
	}
	build, err := buildinfo.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	logger := httplog.NewLogger(os.Args[1], build.Version)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	err = run(ctx, build, logger)
	stop()
	if err != nil {
		logger.Error("service failed", "error", err.Error())
		os.Exit(1)
	}
}

func dataDir() string {
	if dir := os.Getenv("DATA_DIR"); dir != "" {
		return dir
	}
	return "/data"
}

func openStore(ctx context.Context, build buildinfo.Info) (*store.Store, error) {
	return store.Open(ctx, os.Getenv("DATABASE_URL"), build.Level, databaseWait)
}

func runAPI(ctx context.Context, build buildinfo.Info, logger *slog.Logger) error {
	db, err := openStore(ctx, build)
	if err != nil {
		return err
	}
	defer db.Close()
	cfg := api.Config{Build: build, AdminToken: os.Getenv("ADMIN_TOKEN"), DataDir: dataDir()}
	return api.Run(ctx, cfg, db, os.Getenv("DATABASE_URL"), logger)
}

func runWorker(ctx context.Context, build buildinfo.Info, logger *slog.Logger) error {
	db, err := openStore(ctx, build)
	if err != nil {
		return err
	}
	defer db.Close()
	w := &worker.Worker{
		DB: db, Level: build.Level, DataDir: dataDir(), Logger: logger, Now: time.Now,
	}
	logger.Info("worker started", "data_dir", w.DataDir)
	w.Run(ctx)
	logger.Info("stopped")
	return nil
}

func runMigrate(ctx context.Context, build buildinfo.Info, logger *slog.Logger) error {
	db, err := openStore(ctx, build)
	if err != nil {
		return err
	}
	defer db.Close()
	result, err := db.Migrate(ctx, time.Now())
	if err != nil {
		return err
	}
	logger.Info("migration done", "schema_before", result.Before, "schema_after", result.After,
		"applied", result.Applied, "build_schema", build.Level)
	return nil
}

func runLoadgen(ctx context.Context, _ buildinfo.Info, logger *slog.Logger) error {
	generator, err := loadgen.New(os.Getenv("API_URL"), logger)
	if err != nil {
		return err
	}
	logger.Info("loadgen started", "target", generator.BaseURL)
	generator.Run(ctx)
	logger.Info("stopped")
	return nil
}

func runWeb(ctx context.Context, build buildinfo.Info, logger *slog.Logger) error {
	server, err := web.New(build, os.Getenv("API_URL"), logger)
	if err != nil {
		return err
	}
	return httplog.Serve(ctx, web.Addr, server.Handler(), logger)
}
