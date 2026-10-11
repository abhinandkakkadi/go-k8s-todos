package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"go-k8s-todos/internal/server"
)

// version is set at build time: -ldflags "-X main.version=$SHA".
var version = "dev"

const (
	// drainDelay gives Kubernetes time to remove the pod from Service
	// endpoints after SIGTERM, so no new requests arrive once we stop
	// accepting connections.
	drainDelay = 5 * time.Second

	// shutdownTimeout bounds how long in-flight requests may take to finish.
	// drainDelay + shutdownTimeout must stay below terminationGracePeriodSeconds (30s).
	shutdownTimeout = 20 * time.Second
)

func main() {
	app := server.New(version)
	srv := &http.Server{
		Addr:              ":8080",
		Handler:           app.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		log.Printf("Go API %s listening on :8080", version)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		log.Fatal(err)
	case <-ctx.Done():
	}
	stop()

	log.Println("Shutdown signal received, draining")
	app.SetNotReady()
	time.Sleep(drainDelay)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Graceful shutdown failed: %v", err)
	}
	log.Println("Server stopped")
}
