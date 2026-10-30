package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/FarhanHan23/task-manager-go/internal/platform/database"
	"github.com/FarhanHan23/task-manager-go/internal/platform/web"
	"github.com/FarhanHan23/task-manager-go/internal/todo"
)

// version diisi saat build: go build -ldflags "-X main.version=1.0.0"
var version = "dev"

// config dibaca dari environment variable (gaya 12-factor app).
type config struct {
	Addr            string
	DBPath          string
	LogLevel        slog.Level
	ShutdownTimeout time.Duration
}

func loadConfig() (config, error) {
	cfg := config{
		Addr:            getEnv("APP_ADDR", ":8080"),
		DBPath:          getEnv("APP_DB_PATH", "todo.db"),
		ShutdownTimeout: 10 * time.Second,
	}
	if err := cfg.LogLevel.UnmarshalText([]byte(getEnv("APP_LOG_LEVEL", "info"))); err != nil {
		return config{}, fmt.Errorf("APP_LOG_LEVEL tidak valid: %w", err)
	}
	if v := os.Getenv("APP_SHUTDOWN_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return config{}, fmt.Errorf("APP_SHUTDOWN_TIMEOUT tidak valid: %w", err)
		}
		cfg.ShutdownTimeout = d
	}
	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return v
	}
	return fallback
}

func main() {
	// main hanya memanggil run; semua error dikembalikan ke sini.
	if err := run(); err != nil {
		slog.Error("aplikasi berhenti karena error", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	slog.SetDefault(logger)

	// ctx akan dibatalkan saat menerima Ctrl+C (SIGINT) atau SIGTERM (docker stop).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// ---- Database ----
	db, err := database.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()

	n, err := database.Migrate(ctx, db)
	if err != nil {
		return err
	}
	logger.Info("migrasi selesai", "diterapkan", n)

	// ---- Wiring (dependency injection manual) ----
	repo := todo.NewSQLiteRepository(db)
	svc := todo.NewService(repo)
	h := todo.NewHandler(svc, logger)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if err := db.PingContext(r.Context()); err != nil {
			web.WriteError(w, http.StatusServiceUnavailable, "unavailable", "database tidak siap", nil)
			return
		}
		web.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": version})
	})

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           web.Chain(mux, web.Recover(logger), web.Logging(logger)),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// ---- Jalankan server di goroutine terpisah ----
	serverErr := make(chan error, 1)
	go func() {
		logger.Info("server berjalan", "addr", cfg.Addr, "version", version)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
		close(serverErr)
	}()

	// ---- Tunggu: sinyal berhenti ATAU server gagal start ----
	select {
	case err := <-serverErr:
		return fmt.Errorf("server gagal: %w", err)
	case <-ctx.Done():
		logger.Info("sinyal berhenti diterima, mematikan server...")
	}

	// ---- Graceful shutdown ----
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	logger.Info("server berhenti dengan rapi")
	return nil
}
