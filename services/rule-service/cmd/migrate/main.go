package main

import (
	"context"
	"log/slog"
	"os"

	"predixaai-backend/services/rule-service/internal/storage"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		logger.Error("DATABASE_URL is required")
		os.Exit(1)
	}
	ctx := context.Background()
	store, err := storage.NewStore(ctx, dsn)
	if err != nil {
		logger.Error("failed to connect", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer store.Close()

	if err := store.RunMigrations(ctx); err != nil {
		logger.Error("failed to run migrations", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("all migrations applied")
}

