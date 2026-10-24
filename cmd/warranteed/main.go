package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
	_ "time/tzdata" // the runtime image has no zoneinfo; TZ must still work

	"github.com/fhavrlent/warranteed/internal/api"
	"github.com/fhavrlent/warranteed/internal/auth"
	"github.com/fhavrlent/warranteed/internal/config"
	"github.com/fhavrlent/warranteed/internal/db"
	"github.com/fhavrlent/warranteed/internal/notify"
	"github.com/fhavrlent/warranteed/internal/store"
	"github.com/fhavrlent/warranteed/web"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}

	level := slog.LevelInfo
	if os.Getenv("WARRANTEED_DEBUG") == "true" {
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level})))

	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration:\n%w", err)
	}
	if cfg.DevLogin {
		slog.Warn("WARRANTEED_DEV_LOGIN is enabled: anyone can sign in without a password. Never use this in production.")
	}

	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return err
	}
	database, err := db.Open(filepath.Join(cfg.DataDir, "warranteed.db"))
	if err != nil {
		return err
	}
	defer database.Close()

	st := store.New(database)
	files, err := api.NewFiles(cfg.FilesDir)
	if err != nil {
		return err
	}
	notifier := notify.New(st, cfg.Location, cfg.NotifyHour, cfg.AppURL.String(), notify.Policy{AllowedHosts: cfg.NotifyAllowedHosts})
	key, err := auth.LoadKey(cfg.Secret, cfg.DataDir)
	if err != nil {
		return err
	}
	srv := api.New(cfg, st, auth.New(cfg, st, key), notifier, files, web.Assets(), version)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go srv.PollPaperless(ctx)
	go notifier.Run(ctx)
	go func() {
		t := time.NewTicker(6 * time.Hour)
		defer t.Stop()
		for {
			st.PurgeExpiredSessions(ctx)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()

	httpSrv := &http.Server{
		Addr:              cfg.Listen,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}
	errc := make(chan error, 1)
	go func() {
		slog.Info("warranteed listening", "addr", cfg.Listen, "url", cfg.AppURL.String(), "version", version)
		errc <- httpSrv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		slog.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutdownCtx)
	}
	return nil
}

// healthcheck is used by the Docker HEALTHCHECK (the image has no curl).
func healthcheck() int {
	addr := os.Getenv("WARRANTEED_LISTEN")
	if addr == "" {
		addr = ":8080"
	}
	if addr[0] == ':' {
		addr = "127.0.0.1" + addr
	}
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + addr + "/healthz")
	if err != nil || resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}
