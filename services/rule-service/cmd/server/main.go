package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"predixaai-backend/services/rule-service/internal/api"
	"predixaai-backend/services/rule-service/internal/bus"
	"predixaai-backend/services/rule-service/internal/crypto"
	"predixaai-backend/services/rule-service/internal/storage"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	port := getenv("PORT", "8090")
	dsn := getenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/rules?sslmode=disable")
	natsURL := getenv("NATS_URL", "nats://localhost:4222")
	minPoll := getenvInt("RULE_POLL_MIN", 5)
	maxPoll := getenvInt("RULE_POLL_MAX", 3600)
	dbConnectorURL := getenv("DB_CONNECTOR_URL", "http://localhost:8085")
	schedulerURL := getenv("SCHEDULER_ADMIN_URL", "http://localhost:8091")
	key := getenv("ENCRYPTION_KEY", "")
	if len(key) != 32 {
		logger.Error("ENCRYPTION_KEY must be 32 bytes")
		os.Exit(1)
	}
	enc, err := crypto.NewAesGcmEncryptor([]byte(key))
	if err != nil {
		logger.Error("failed to init encryptor", slog.String("error", err.Error()))
		os.Exit(1)
	}
	ctx := context.Background()
	store, err := storage.NewStore(ctx, dsn)
	if err != nil {
		logger.Error("failed to connect to db", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer store.Close()
	if err := store.RunMigrations(ctx); err != nil {
		logger.Error("failed to run migrations", slog.String("error", err.Error()))
		os.Exit(1)
	}
	logger.Info("migrations applied")
	repo := storage.NewRepository(store)
	publisher, err := bus.NewPublisher(natsURL)
	if err != nil {
		logger.Error("failed to connect to nats", slog.String("error", err.Error()))
		os.Exit(1)
	}
	defer publisher.Close()

	handler := &api.Handler{
		Repo:      repo,
		Bus:       publisher,
		Encryptor: enc,
		MinPoll:   minPoll,
		MaxPoll:   maxPoll,
		Timeout:   5 * time.Second,
		DBConnectorURL: dbConnectorURL,
		SchedulerURL:   schedulerURL,
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(10 * time.Second))

	handler.RegisterRoutes(r)

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  30 * time.Second,
	}

	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-shutdown
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	logger.Info("rule-service listening", slog.String("port", port))
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		logger.Error("server error", slog.String("error", err.Error()))
	}
}

func getenv(key, fallback string) string {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	return val
}

func getenvInt(key string, fallback int) int {
	val := os.Getenv(key)
	if val == "" {
		return fallback
	}
	if parsed, err := strconv.Atoi(val); err == nil {
		return parsed
	}
	return fallback
}
