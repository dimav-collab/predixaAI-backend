package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	dbconnector "predixaai-backend"
	"predixaai-backend/cmd/service/internal/connections"
)

func main() {
	port := getenv("PORT", "8080")
	resolver, resolverErr := connections.NewResolverFromEnv()
	if resolverErr != nil {
		log.Printf("connectionRef resolver disabled: %v", resolverErr)
	}
	h := NewHandler(resolver, dbconnector.NewConnector)
	mux := http.NewServeMux()
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/connection/test", h.HandleTestConnection)
	mux.HandleFunc("/tables", h.HandleListTables)
	mux.HandleFunc("/describe", h.HandleDescribeTable)
	mux.HandleFunc("/sample", h.HandleSampleRows)
	mux.HandleFunc("/profile", h.HandleProfileTable)
	mux.HandleFunc("/simulate/write", h.HandleSimulateWrite)

	server := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	shutdownErr := make(chan error, 1)
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		shutdownErr <- server.Shutdown(ctx)
	}()

	log.Printf("db-connector service listening on :%s", port)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server error: %v", err)
	}

	if err := <-shutdownErr; err != nil {
		log.Fatalf("shutdown error: %v", err)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}
