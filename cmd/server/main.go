package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/susanta96/toolbox-backend/internal/config"
	"github.com/susanta96/toolbox-backend/internal/database"
	"github.com/susanta96/toolbox-backend/internal/handler"
	"github.com/susanta96/toolbox-backend/internal/repository"
	"github.com/susanta96/toolbox-backend/internal/scheduler"
	"github.com/susanta96/toolbox-backend/internal/service"
)

func main() {
	// Structured logging
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	// Load .env file if present (ignored in production where real env vars are set)
	_ = godotenv.Load()

	cfg := config.Load()

	// Database
	ctx := context.Background()
	pool, err := database.New(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := database.RunMigrations(ctx, pool); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	// Services
	pdfService := service.NewPDFService(cfg.UploadDir, cfg.GeneratedDir)
	if err := pdfService.CheckQPDF(); err != nil {
		slog.Error("qpdf is required but not found", "error", err)
		os.Exit(1)
	}
	if err := pdfService.CheckGhostscript(); err != nil {
		slog.Error("ghostscript is required but not found", "error", err)
		os.Exit(1)
	}
	if err := pdfService.EnsureDirectories(); err != nil {
		slog.Error("failed to create directories", "error", err)
		os.Exit(1)
	}

	// Repository
	fileRepo := repository.NewFileRecordRepository(pool)

	// Handlers
	pdfHandler := handler.NewPDFHandler(pdfService, fileRepo, cfg.UploadDir, cfg.FileRetention, cfg.MaxMergeFiles)
	maxBodyBytes := cfg.MaxUploadSizeMB * 1024 * 1024
	router := handler.NewRouter(pdfHandler, maxBodyBytes)

	// Cleanup scheduler — removes expired files from disk + expired DB records
	cleanup := scheduler.NewCleanup(fileRepo, []string{cfg.UploadDir, cfg.GeneratedDir}, cfg.FileRetention)
	if err := cleanup.Start(cfg.CleanupInterval); err != nil {
		slog.Error("failed to start cleanup scheduler", "error", err)
		os.Exit(1)
	}

	// HTTP server
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Graceful shutdown
	go func() {
		slog.Info("server starting", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	sig := <-quit
	slog.Info("shutting down server", "signal", sig.String())

	cleanup.Stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("server forced shutdown", "error", err)
		os.Exit(1)
	}

	slog.Info("server stopped gracefully")
}
