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
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type workerConfig struct {
	controlPlane   string
	apiToken       string
	name           string
	pollInterval   time.Duration
	autoComplete   bool
	completionMsg  string
	workspace      string
	registryPrefix string
	pushImages     bool
	keepWorkspace  bool
	githubToken    string
}

func main() {
	cfg := parseFlags()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := os.MkdirAll(cfg.workspace, 0o755); err != nil {
		log.Fatalf("workspace setup failed: %v", err)
	}

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
	workspace := flag.String("workspace", envOrDefault("BUILD_WORKER_WORKSPACE", "./worker-tmp"), "Workspace for builds")
	registry := flag.String("registry", os.Getenv("BUILD_WORKER_REGISTRY"), "Registry prefix (e.g. ghcr.io/org)")
	push := flag.Bool("push", false, "Push built images to registry")
	keep := flag.Bool("keep-workspace", false, "Keep workspace after builds")
	ghToken := flag.String("github-token", os.Getenv("GITHUB_TOKEN"), "GitHub token for cloning private repos")
	flag.Parse()

	return workerConfig{
		controlPlane:   strings.TrimRight(*control, "/"),
		apiToken:       *token,
		name:           *name,
		pollInterval:   *interval,
		autoComplete:   *auto,
		completionMsg:  *reason,
		workspace:      *workspace,
		registryPrefix: strings.TrimRight(*registry, "/"),
		pushImages:     *push,
		keepWorkspace:  *keep,
		githubToken:    *ghToken,
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

			if err := processJob(ctx, client, cfg, job); err != nil {
				log.Printf("job %s failed: %v", job.ID, err)
			}
		}
	}
}

type buildJob struct {
	ID           string   `json:"id"`
	Repository   string   `json:"repository"`
	Ref          string   `json:"ref"`
	Commit       string   `json:"commit"`
	Installation string   `json:"installation"`
	Status       string   `json:"status"`
	ServiceID    string   `json:"service_id"`
	Environment  string   `json:"environment"`
	ComposePath  string   `json:"compose_path"`
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

func processJob(ctx context.Context, client *http.Client, cfg workerConfig, job *buildJob) error {
	log.Printf("claimed job %s (%s @ %s)", job.ID, job.Repository, job.Commit)

	artifacts, composeData, err := performBuild(ctx, cfg, job)
	if err != nil {
		updateJob(ctx, client, cfg, job.ID, "failed", fmt.Sprintf("build error: %v", err), nil, "")
		return err
	}

	if job.ServiceID != "" {
		if len(composeData) > 0 {
			if err := applyCompose(ctx, client, cfg, job, composeData); err != nil {
				log.Printf("compose upload failed: %v", err)
			}
		}
		if len(artifacts) > 0 {
			if err := applyDeployment(ctx, client, cfg, job, artifacts[0]); err != nil {
				updateJob(ctx, client, cfg, job.ID, "failed", fmt.Sprintf("deploy error: %v", err), nil, job.ComposePath)
				return err
			}
		}
	}

	status := "running"
	reason := cfg.completionMsg
	artifactList := artifacts
	if cfg.autoComplete {
		status = "succeeded"
	} else {
		artifactList = nil
	}
	if err := updateJob(ctx, client, cfg, job.ID, status, reason, artifactList, job.ComposePath); err != nil {
		log.Printf("update job error: %v", err)
	}
	return nil
}

func updateJob(ctx context.Context, client *http.Client, cfg workerConfig, id, status, reason string, artifacts []string, composePath string) error {
	payload := map[string]any{}
	if status != "" {
		payload["status"] = status
	}
	if reason != "" {
		payload["reason"] = reason
	}
	if len(artifacts) > 0 {
		payload["artifacts"] = artifacts
	}
	if composePath != "" {
		payload["compose_path"] = composePath
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

func performBuild(ctx context.Context, cfg workerConfig, job *buildJob) ([]string, []byte, error) {
	owner, name, err := splitRepo(job.Repository)
	if err != nil {
		return nil, nil, err
	}

	workdir := filepath.Join(cfg.workspace, job.ID)
	if err := os.RemoveAll(workdir); err != nil {
		return nil, nil, fmt.Errorf("clean workspace: %w", err)
	}

	cloneURL := fmt.Sprintf("https://github.com/%s.git", job.Repository)
	if cfg.githubToken != "" {
		cloneURL = fmt.Sprintf("https://%s@github.com/%s.git", cfg.githubToken, job.Repository)
	}

	if err := runCommand(ctx, cfg, cfg.workspace, gitEnv(), "git", "clone", "--depth", "1", cloneURL, workdir); err != nil {
		return nil, nil, fmt.Errorf("git clone: %w", err)
	}

	if !cfg.keepWorkspace {
		defer os.RemoveAll(workdir)
	}

	if err := runCommand(ctx, cfg, workdir, gitEnv(), "git", "fetch", "--depth", "1", "origin", job.Commit); err != nil {
		return nil, nil, fmt.Errorf("git fetch: %w", err)
	}

	if err := runCommand(ctx, cfg, workdir, gitEnv(), "git", "checkout", job.Commit); err != nil {
		return nil, nil, fmt.Errorf("git checkout: %w", err)
	}

	if cfg.githubToken != "" {
		_ = runCommand(ctx, cfg, workdir, gitEnv(), "git", "remote", "set-url", "origin", fmt.Sprintf("https://github.com/%s.git", job.Repository))
	}

	var composeData []byte
	if job.ComposePath != "" {
		fullPath := filepath.Join(workdir, job.ComposePath)
		if data, err := os.ReadFile(fullPath); err == nil {
			composeData = data
		} else {
			log.Printf("[worker %s] compose file not found: %s (%v)", cfg.name, job.ComposePath, err)
		}
	}

	prefix := cfg.registryPrefix
	if prefix == "" {
		prefix = fmt.Sprintf("ghcr.io/%s", strings.ToLower(owner))
	}
	prefix = strings.TrimSuffix(prefix, "/")
	imageName := fmt.Sprintf("%s/%s:%s", prefix, strings.ToLower(name), shortSHA(job.Commit))

	log.Printf("[worker %s] docker build %s", cfg.name, imageName)
	if err := runCommand(ctx, cfg, workdir, dockerEnv(), "docker", "build", "-t", imageName, "."); err != nil {
		return nil, nil, fmt.Errorf("docker build: %w", err)
	}

	if cfg.pushImages {
		log.Printf("[worker %s] docker push %s", cfg.name, imageName)
		if err := runCommand(ctx, cfg, "", nil, "docker", "push", imageName); err != nil {
			return nil, nil, fmt.Errorf("docker push: %w", err)
		}
	}

	if !cfg.autoComplete {
		log.Printf("[worker %s] auto-complete disabled; leaving job running", cfg.name)
	}

	return []string{imageName}, composeData, nil
}

func applyDeployment(ctx context.Context, client *http.Client, cfg workerConfig, job *buildJob, image string) error {
	if job.ServiceID == "" {
		return nil
	}
	payload := map[string]string{
		"environment": job.Environment,
		"image":       image,
	}
	if payload["environment"] == "" {
		payload["environment"] = "production"
	}
	data, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("%s/v1/services/%s/deployments", cfg.controlPlane, job.ServiceID), bytes.NewReader(data))
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
		return fmt.Errorf("deploy call error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func applyCompose(ctx context.Context, client *http.Client, cfg workerConfig, job *buildJob, compose []byte) error {
	if job.ServiceID == "" || len(compose) == 0 {
		return nil
	}
	payload := map[string]string{"compose": string(compose)}
	data, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, fmt.Sprintf("%s/v1/service-compose/%s", cfg.controlPlane, job.ServiceID), bytes.NewReader(data))
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
		return fmt.Errorf("compose update error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func runCommand(ctx context.Context, cfg workerConfig, dir string, extraEnv []string, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	env := append(os.Environ(), extraEnv...)
	cmd.Env = env
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	log.Printf("[worker %s] exec: %s", cfg.name, sanitizeCommand(cfg.githubToken, name, args))
	return cmd.Run()
}

func sanitizeCommand(secret string, name string, args []string) string {
	parts := append([]string{name}, args...)
	cmd := strings.Join(parts, " ")
	if secret != "" {
		cmd = strings.ReplaceAll(cmd, secret, "****")
	}
	return cmd
}

func splitRepo(repo string) (string, string, error) {
	parts := strings.Split(repo, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid repository: %s", repo)
	}
	owner := strings.TrimSpace(parts[0])
	name := strings.TrimSpace(parts[1])
	if owner == "" || name == "" {
		return "", "", fmt.Errorf("invalid repository: %s", repo)
	}
	return owner, name, nil
}

func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

func gitEnv() []string {
	return []string{"GIT_TERMINAL_PROMPT=0"}
}

func dockerEnv() []string {
	return []string{"DOCKER_BUILDKIT=1"}
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
