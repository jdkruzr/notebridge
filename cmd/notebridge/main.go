package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sysop/notebridge/internal/config"
	"github.com/sysop/notebridge/internal/sync"
	"github.com/sysop/notebridge/internal/syncdb"
)

func main() {
	// Load config from environment
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Set up structured logging (JSON handler for now)
	var logLevel slog.Level
	if err := logLevel.UnmarshalText([]byte(cfg.LogLevel)); err != nil {
		logLevel = slog.LevelInfo
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	// Open SQLite database
	db, err := syncdb.Open(cfg.DBPath)
	if err != nil {
		logger.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	logger.Info("database opened", "path", cfg.DBPath)

	// Create store
	store := syncdb.NewStore(db)

	// Bootstrap user with EnsureUser
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := store.EnsureUser(ctx, cfg.UserEmail, cfg.UserPasswordHash, nil); err != nil {
		logger.Error("failed to bootstrap user", "error", err)
		os.Exit(1)
	}

	logger.Info("user bootstrapped", "email", cfg.UserEmail)

	// Get or create JWT secret from store
	// Note: cfg.JWTSecret is already set from env (required by config.Load)
	// We store it in the DB for retrieval by AuthService
	_, err = store.GetOrCreateJWTSecret(ctx)
	if err != nil {
		logger.Error("failed to get or create JWT secret", "error", err)
		os.Exit(1)
	}

	// Create SnowflakeGenerator
	snowflake := sync.NewSnowflakeGenerator()

	// Create AuthService
	authService := sync.NewAuthService(store, snowflake)

	// Create sync.Server
	server := sync.NewServer(store, authService, logger)

	// Create HTTP server
	httpServer := &http.Server{
		Addr:         cfg.SyncListenAddr,
		Handler:      server.Handler(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start HTTP server in a goroutine
	go func() {
		logger.Info("starting sync server", "addr", cfg.SyncListenAddr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server error", "error", err)
		}
	}()

	// Set up graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	sig := <-sigChan
	logger.Info("received signal", "signal", sig)

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("failed to shutdown http server", "error", err)
		os.Exit(1)
	}

	logger.Info("sync server stopped gracefully")
}
