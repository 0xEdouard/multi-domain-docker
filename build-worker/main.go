package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

type workerConfig struct {
	controlPlane string
	apiToken     string
	name         string
	pollInterval time.Duration
	autoComplete bool
	completionMsg string
}

func main() {
	cfg := parseFlags()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, cfg); err != nil {
		log.Fatalf("worker error: %v", err)
	}
}

func parseFlags() workerConfig {
	control := flag.String("control-plane", envOrDefault("CONTROL_PLANE_URL", "http://localhost:8080"), "Control plane base URL")
	token := flag.String("token", os.Getenv("CONTROL_PLANE_TOKEN"), "Bearer token")
	name := flag.String("name", envOrDefault("BUILD_WORKER_NAME", "builder-local"), "Worker identifier")
	interval := flag.Duration("interval", 5*time.Second, "Polling interval")
	auto := flag.Bool("auto-complete", false, "Automatically mark jobs as succeeded")
	reason := flag.String("reason", "", "Completion reason/message")
	flag.Parse()

	return workerConfig{
		controlPlane: strings.TrimRight(*control, "/"),
		apiToken:     *token,
		name:         *name,
		pollInterval: *interval,
		autoComplete: *auto,
		completionMsg: *reason,
	}
}

func run(ctx context.Context, cfg workerConfig) error {
	client := &http.Client{Timeout: 30 * time.Second}
	log.Printf("build worker %s polling %s every %s", cfg.name, cfg.controlPlane, cfg.pollInterval)
	ticker := time.NewTicker(cfg.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("worker exiting")
			return nil
		case <-ticker.C:
			job, err := claimJob(ctx, client, cfg)
			if err != nil {
				log.Printf("claim error: %v", err)
				continue
			}
			if job == nil {
				continue
			}

			log.Printf("claimed job %s (%s @ %s)", job.ID, job.Repository, job.Commit)

			if err := performBuild(job, cfg); err != nil {
				log.Printf("job %s failed: %v", job.ID, err)
				if err := updateJob(ctx, client, cfg, job.ID, "failed", err.Error()); err != nil {
					log.Printf("report failure error: %v", err)
				}
				continue
			}

			status := "running"
			reason := cfg.completionMsg
			if cfg.autoComplete {
				status = "succeeded"
			}
			if err := updateJob(ctx, client, cfg, job.ID, status, reason); err != nil {
				log.Printf("update job error: %v", err)
			}
		}
	}
}

type buildJob struct {
	ID           string `json:"id"`
	Repository   string `json:"repository"`
	Ref          string `json:"ref"`
	Commit       string `json:"commit"`
	Installation string `json:"installation"`
	Status       string `json:"status"`
}

func claimJob(ctx context.Context, client *http.Client, cfg workerConfig) (*buildJob, error) {
	body := map[string]string{"worker": cfg.name}
	payload, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.controlPlane+"/v1/build-jobs/claim", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.apiToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("claim error %d: %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	var job buildJob
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, err
	}
	return &job, nil
}

func updateJob(ctx context.Context, client *http.Client, cfg workerConfig, id, status, reason string) error {
	payload := map[string]string{}
	if status != "" {
		payload["status"] = status
	}
	if reason != "" {
		payload["reason"] = reason
	}
	data, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, cfg.controlPlane+"/v1/build-jobs/"+id, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if cfg.apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.apiToken)
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("update job error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func performBuild(job *buildJob, cfg workerConfig) error {
	// Placeholder build logic; will be replaced with actual git checkout + docker build.
	log.Printf("[worker %s] building %s (%s) at %s", cfg.name, job.Repository, job.Ref, job.Commit)
	if !cfg.autoComplete {
		log.Printf("[worker %s] auto-complete disabled; leaving job in running state", cfg.name)
	}
	return nil
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
