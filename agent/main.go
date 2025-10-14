package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
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
	reconcileInterval time.Duration
	composeDir      string
}

func main() {
	cfg := parseFlags()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := os.MkdirAll(filepath.Dir(cfg.outputPath), 0o755); err != nil {
		log.Fatalf("output dir: %v", err)
	}
	if err := os.MkdirAll(cfg.composeDir, 0o755); err != nil {
		log.Fatalf("compose dir: %v", err)
	}

	if err := run(ctx, cfg); err != nil {
		log.Fatalf("agent error: %v", err)
	}
}

func parseFlags() agentConfig {
	controlPlane := flag.String("control-plane", envOrDefault("CONTROL_PLANE_URL", "http://localhost:8080"), "Base URL for control plane")
	token := flag.String("token", os.Getenv("CONTROL_PLANE_TOKEN"), "Bearer token")
	output := flag.String("traefik-file", envOrDefault("TRAEFIK_DYNAMIC_PATH", "./traefik.yml"), "Traefik config output path")
	cfgInterval := flag.Duration("poll-interval", 15*time.Second, "Traefik config polling interval")
	reconcile := flag.Duration("deploy-interval", 20*time.Second, "Container reconcile interval")
	composeDir := flag.String("compose-dir", envOrDefault("AGENT_COMPOSE_DIR", "./compose"), "Directory for rendered docker-compose files")
	flag.Parse()

	return agentConfig{
		controlPlaneURL: strings.TrimRight(*controlPlane, "/"),
		apiToken:        *token,
		outputPath:      *output,
		pollInterval:    *cfgInterval,
		reconcileInterval: *reconcile,
		composeDir:      *composeDir,
	}
}

func run(ctx context.Context, cfg agentConfig) error {
	var lastHash [32]byte
	configTicker := time.NewTicker(cfg.pollInterval)
	defer configTicker.Stop()
	reconcileTicker := time.NewTicker(cfg.reconcileInterval)
	defer reconcileTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("agent shutting down")
			return nil
		case <-configTicker.C:
			if err := updateTraefikConfig(ctx, cfg, &lastHash); err != nil {
				log.Printf("traefik update error: %v", err)
			}
		case <-reconcileTicker.C:
			if err := reconcileServices(ctx, cfg); err != nil {
				log.Printf("reconcile error: %v", err)
			}
		}
	}
}

func updateTraefikConfig(ctx context.Context, cfg agentConfig, lastHash *[32]byte) error {
	config, err := fetchTraefikConfig(ctx, cfg)
	if err != nil {
		return err
	}
	hash := sha256.Sum256(config)
	if hash == *lastHash {
		return nil
	}

	if err := os.WriteFile(cfg.outputPath, config, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	*lastHash = hash
	log.Printf("updated Traefik config at %s (%d bytes)", cfg.outputPath, len(config))
	return nil
}

func fetchTraefikConfig(ctx context.Context, cfg agentConfig) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.controlPlaneURL+"/v1/traefik/config", nil)
	if err != nil {
		return nil, err
	}
	if cfg.apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.apiToken)
	}

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

type serviceState struct {
	ID           string `json:"id"`
	ProjectID    string `json:"project_id"`
	Name         string `json:"name"`
	Image        string `json:"image"`
	InternalPort int    `json:"internal_port"`
	Compose      string `json:"compose"`
}

func fetchServiceState(ctx context.Context, cfg agentConfig) ([]serviceState, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, cfg.controlPlaneURL+"/v1/state/services", nil)
	if err != nil {
		return nil, err
	}
	if cfg.apiToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.apiToken)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		buf, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("service state error %d: %s", resp.StatusCode, string(bytes.TrimSpace(buf)))
	}

	var payload struct {
		Services []serviceState `json:"services"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.Services, nil
}

func reconcileServices(ctx context.Context, cfg agentConfig) error {
	services, err := fetchServiceState(ctx, cfg)
	if err != nil {
		return err
	}

	desired := make(map[string]serviceState)
	for _, svc := range services {
		desired[svc.ID] = svc
		if err := ensureService(ctx, cfg, svc); err != nil {
			log.Printf("service %s ensure failed: %v", svc.ID, err)
		}
	}

	return cleanupServices(ctx, cfg, desired)
}

func ensureService(ctx context.Context, cfg agentConfig, svc serviceState) error {
	if strings.TrimSpace(svc.Compose) != "" {
		return ensureComposeService(ctx, cfg, svc)
	}
	return ensureContainer(ctx, cfg, svc)
}

func ensureContainer(ctx context.Context, cfg agentConfig, svc serviceState) error {
	if strings.TrimSpace(svc.Image) == "" {
		return nil
	}
	container := "svc-" + svc.ID
	composePath := filepath.Join(cfg.composeDir, svc.ID, "docker-compose.yml")
	if _, err := os.Stat(composePath); err == nil {
		log.Printf("compose stack detected for %s, bringing it down", svc.ID)
		_ = runDocker(ctx, "compose", "-f", composePath, "-p", "mdp-"+svc.ID, "down", "--remove-orphans")
	}

	if err := runDocker(ctx, "pull", svc.Image); err != nil {
		log.Printf("pull warning for %s: %v", svc.Image, err)
	}

	image, err := dockerInspect(ctx, container, "{{.Config.Image}}")
	if err == nil && strings.TrimSpace(image) == svc.Image {
		state, err := dockerInspect(ctx, container, "{{.State.Running}}")
		if err == nil && strings.TrimSpace(state) == "true" {
			return nil
		}
		return runDocker(ctx, "start", container)
	}

	_ = runDocker(ctx, "rm", "-f", container)

	args := []string{
		"run", "-d",
		"--restart", "unless-stopped",
		"--name", container,
		"--label", "mdp.service=" + svc.ID,
		"-p", fmt.Sprintf("127.0.0.1:%d:%d", svc.InternalPort, svc.InternalPort),
		svc.Image,
	}
	return runDocker(ctx, args...)
}

func ensureComposeService(ctx context.Context, cfg agentConfig, svc serviceState) error {
    _ = runDocker(ctx, "rm", "-f", "svc-"+svc.ID)
	dir := filepath.Join(cfg.composeDir, svc.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("compose dir: %w", err)
	}

	composePath := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(svc.Compose), 0o644); err != nil {
		return fmt.Errorf("write compose: %w", err)
	}

	if err := runDocker(ctx, "compose", "-f", composePath, "-p", "mdp-"+svc.ID, "pull"); err != nil {
		log.Printf("compose pull warning: %v", err)
	}
	return runDocker(ctx, "compose", "-f", composePath, "-p", "mdp-"+svc.ID, "up", "-d", "--remove-orphans")
}

func cleanupServices(ctx context.Context, cfg agentConfig, desired map[string]serviceState) error {
	output, err := runDockerOutput(ctx, "ps", "-a", "--filter", "label=mdp.service", "--format", "{{.Names}} {{.Label \"mdp.service\"}}")
	if err == nil {
		lines := strings.Split(strings.TrimSpace(output), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts := strings.Fields(line)
			if len(parts) != 2 {
				continue
			}
			name := parts[0]
			id := parts[1]
			if _, ok := desired[id]; ok {
				continue
			}
			log.Printf("removing stale container %s", name)
			_ = runDocker(ctx, "rm", "-f", name)
		}
	}

	dirs, err := os.ReadDir(cfg.composeDir)
	if err == nil {
		for _, entry := range dirs {
			if !entry.IsDir() {
				continue
			}
			id := entry.Name()
			if _, ok := desired[id]; ok {
				continue
			}
			composePath := filepath.Join(cfg.composeDir, id, "docker-compose.yml")
			if _, statErr := os.Stat(composePath); statErr == nil {
				log.Printf("bringing down compose stack for service %s", id)
				_ = runDocker(ctx, "compose", "-f", composePath, "-p", "mdp-"+id, "down", "--remove-orphans")
			}
		}
	}
	return nil
}

func runDocker(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runDockerOutput(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func dockerInspect(ctx context.Context, container string, format string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", "inspect", "--format", format, container)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &bytes.Buffer{}
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
