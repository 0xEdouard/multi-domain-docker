package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type agentConfig struct {
	controlPlaneURL string
	apiToken        string
	outputPath      string
	pollInterval    time.Duration
}

func main() {
	cfg := parseFlags()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, cfg); err != nil {
		log.Fatalf("agent error: %v", err)
	}
}

func parseFlags() agentConfig {
	controlPlane := flag.String("control-plane", envOrDefault("CONTROL_PLANE_URL", "http://localhost:8080"), "Base URL for the control plane API")
	token := flag.String("token", os.Getenv("CONTROL_PLANE_TOKEN"), "Bearer token for API authentication")
	output := flag.String("traefik-file", envOrDefault("TRAEFIK_DYNAMIC_PATH", "./proxy/dynamic/control-plane.yml"), "Path to write the Traefik dynamic config")
	interval := flag.Duration("poll-interval", 15*time.Second, "Polling interval for control plane updates")
	flag.Parse()

	return agentConfig{
		controlPlaneURL: strings.TrimRight(*controlPlane, "/"),
		apiToken:        *token,
		outputPath:      *output,
		pollInterval:    *interval,
	}
}

func run(ctx context.Context, cfg agentConfig) error {
	log.Printf("agent starting; polling %s every %s", cfg.controlPlaneURL, cfg.pollInterval)

	var lastHash [32]byte
	ticker := time.NewTicker(cfg.pollInterval)
	defer ticker.Stop()

	if err := ensureDir(cfg.outputPath); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			log.Println("agent shutting down")
			return nil
		case <-ticker.C:
			body, err := fetchTraefikConfig(ctx, cfg)
			if err != nil {
				log.Printf("fetch error: %v", err)
				continue
			}

			hash := sha256.Sum256(body)
			if hash == lastHash {
				continue
			}

			if err := os.WriteFile(cfg.outputPath, body, 0600); err != nil {
				log.Printf("write error: %v", err)
				continue
			}

			lastHash = hash
			log.Printf("updated Traefik config at %s (%d bytes)", cfg.outputPath, len(body))
			log.Println("TODO: reconcile Docker containers for new desired state")
		}
	}
}

func fetchTraefikConfig(ctx context.Context, cfg agentConfig) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.controlPlaneURL+"/v1/traefik/config", nil)
	if err != nil {
		return nil, err
	}
	if cfg.apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.apiToken)
	}
	req.Header.Set("Accept", "application/x-yaml")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("control plane response %d: %s", resp.StatusCode, string(bytes.TrimSpace(buf)))
	}

	return io.ReadAll(resp.Body)
}

func ensureDir(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create directory %s: %w", dir, err)
	}
	return nil
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
