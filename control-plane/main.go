package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/0xEdouard/multi-domain-infra/control-plane/internal/api"
	"github.com/0xEdouard/multi-domain-infra/control-plane/internal/store"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	statePath := flag.String("state", "./data/state.json", "path to state file")
	apiToken := flag.String("api-token", "", "API bearer token (optional)")
	leResolver := flag.String("le-resolver", "le", "Traefik cert resolver name")
	flag.Parse()

	if err := os.MkdirAll(filepath.Dir(*statePath), 0755); err != nil {
		log.Fatalf("failed to create state directory: %v", err)
	}

	st, err := store.New(*statePath)
	if err != nil {
		log.Fatalf("failed to init store: %v", err)
	}

	server := api.New(api.Config{
		Store:      st,
		APIToken:   *apiToken,
		LEResolver: *leResolver,
	})

	srv := &http.Server{
		Addr:         *addr,
		Handler:      server.Handler(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Printf("control-plane listening on %s", *addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	log.Println("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
}
