package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

type cliConfig struct {
	baseURL string
	token   string
}

func main() {
	if len(os.Args) < 2 {
		printGlobalUsage()
		os.Exit(1)
	}

	cfg := cliConfig{
		baseURL: strings.TrimRight(envOrDefault("INFRCTL_API", "http://localhost:8080"), "/"),
		token:   os.Getenv("INFRCTL_TOKEN"),
	}

	client := apiClient{config: cfg}

	switch os.Args[1] {
	case "project":
		handleProject(client, os.Args[2:])
	case "service":
		handleService(client, os.Args[2:])
	case "domain":
		handleDomain(client, os.Args[2:])
	case "deploy":
		handleDeploy(client, os.Args[2:])
	case "github":
		handleGitHub(client, os.Args[2:])
	case "builds":
		handleBuilds(client, os.Args[2:])
	case "help", "--help", "-h":
		printGlobalUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printGlobalUsage()
		os.Exit(1)
	}
}

func handleProject(client apiClient, args []string) {
	if len(args) == 0 {
		printProjectUsage()
		os.Exit(1)
	}
	switch args[0] {
	case "create":
		fs := flag.NewFlagSet("project create", flag.ExitOnError)
		name := fs.String("name", "", "Project name")
		slug := fs.String("slug", "", "Project slug (optional)")
		fs.Parse(args[1:])

		if *name == "" {
			fmt.Fprintln(os.Stderr, "--name is required")
			fs.Usage()
			os.Exit(1)
		}

		payload := map[string]string{
			"name": *name,
		}
		if *slug != "" {
			payload["slug"] = *slug
		}

		body, err := client.postJSON("/v1/projects", payload)
		if err != nil {
			exitWithError(err)
		}
		printJSON(body)
	case "list":
		body, err := client.get("/v1/projects")
		if err != nil {
			exitWithError(err)
		}
		printJSON(body)
	default:
		fmt.Fprintf(os.Stderr, "unknown project subcommand: %s\n", args[0])
		printProjectUsage()
		os.Exit(1)
	}
}

func handleService(client apiClient, args []string) {
	if len(args) == 0 {
		printServiceUsage()
		os.Exit(1)
	}
	switch args[0] {
	case "create":
		fs := flag.NewFlagSet("service create", flag.ExitOnError)
		projectID := fs.String("project", "", "Project ID")
		name := fs.String("name", "", "Service name")
		image := fs.String("image", "", "Container image reference (optional)")
		port := fs.Int("port", 80, "Internal service port")
		fs.Parse(args[1:])

		if *projectID == "" || *name == "" {
			fmt.Fprintln(os.Stderr, "--project and --name are required")
			fs.Usage()
			os.Exit(1)
		}

		payload := map[string]any{
			"name":          *name,
			"internal_port": *port,
		}
		if *image != "" {
			payload["image"] = *image
		}

		path := fmt.Sprintf("/v1/projects/%s/services", *projectID)
		body, err := client.postJSON(path, payload)
		if err != nil {
			exitWithError(err)
		}
		printJSON(body)
	case "list":
		fs := flag.NewFlagSet("service list", flag.ExitOnError)
		projectID := fs.String("project", "", "Project ID")
		fs.Parse(args[1:])

		if *projectID == "" {
			fmt.Fprintln(os.Stderr, "--project is required")
			fs.Usage()
			os.Exit(1)
		}

		path := fmt.Sprintf("/v1/projects/%s/services", *projectID)
		body, err := client.get(path)
		if err != nil {
			exitWithError(err)
		}
		printJSON(body)
	default:
		fmt.Fprintf(os.Stderr, "unknown service subcommand: %s\n", args[0])
		printServiceUsage()
		os.Exit(1)
	}
}

func handleDomain(client apiClient, args []string) {
	if len(args) == 0 {
		printDomainUsage()
		os.Exit(1)
	}
	switch args[0] {
	case "add":
		fs := flag.NewFlagSet("domain add", flag.ExitOnError)
		serviceID := fs.String("service", "", "Service ID")
		hostname := fs.String("hostname", "", "Hostname")
		environment := fs.String("env", "production", "Environment name")
		fs.Parse(args[1:])

		if *serviceID == "" || *hostname == "" {
			fmt.Fprintln(os.Stderr, "--service and --hostname are required")
			fs.Usage()
			os.Exit(1)
		}

		payload := map[string]string{
			"hostname":    *hostname,
			"environment": *environment,
		}

		path := fmt.Sprintf("/v1/services/%s/domains", *serviceID)
		body, err := client.postJSON(path, payload)
		if err != nil {
			exitWithError(err)
		}
		printJSON(body)
	default:
		fmt.Fprintf(os.Stderr, "unknown domain subcommand: %s\n", args[0])
		printDomainUsage()
		os.Exit(1)
	}
}

func handleDeploy(client apiClient, args []string) {
	if len(args) == 0 {
		printDeployUsage()
		os.Exit(1)
	}
	switch args[0] {
	case "set":
		fs := flag.NewFlagSet("deploy set", flag.ExitOnError)
		serviceID := fs.String("service", "", "Service ID")
		image := fs.String("image", "", "Container image reference")
		environment := fs.String("env", "production", "Environment name")
		fs.Parse(args[1:])

		if *serviceID == "" || *image == "" {
			fmt.Fprintln(os.Stderr, "--service and --image are required")
			fs.Usage()
			os.Exit(1)
		}

		payload := map[string]string{
			"image":       *image,
			"environment": *environment,
		}

		path := fmt.Sprintf("/v1/services/%s/deployments", *serviceID)
		body, err := client.postJSON(path, payload)
		if err != nil {
			exitWithError(err)
		}
		printJSON(body)
	default:
		fmt.Fprintf(os.Stderr, "unknown deploy subcommand: %s\n", args[0])
		printDeployUsage()
		os.Exit(1)
	}
}

type apiClient struct {
	config cliConfig
}

func (c apiClient) newRequest(method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, c.config.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	if c.config.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.config.token)
	}
	return req, nil
}

func (c apiClient) postJSON(path string, payload any) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := c.newRequest(http.MethodPost, path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	body, status, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, fmt.Errorf("api error %d: %s", status, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func (c apiClient) postJSONStatus(path string, payload any) ([]byte, int, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, err
	}
	req, err := c.newRequest(http.MethodPost, path, bytes.NewReader(data))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	body, status, err := c.do(req)
	if err != nil {
		return nil, status, err
	}
	if status >= 400 {
		return nil, status, fmt.Errorf("api error %d: %s", status, strings.TrimSpace(string(body)))
	}
	return body, status, nil
}

func (c apiClient) patchJSON(path string, payload any) ([]byte, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := c.newRequest(http.MethodPatch, path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	body, status, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, fmt.Errorf("api error %d: %s", status, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func (c apiClient) get(path string) ([]byte, error) {
	req, err := c.newRequest(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	body, status, err := c.do(req)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, fmt.Errorf("api error %d: %s", status, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func (c apiClient) do(req *http.Request) ([]byte, int, error) {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

func printJSON(data []byte) {
	var buf bytes.Buffer
	if err := json.Indent(&buf, data, "", "  "); err != nil {
		fmt.Println(string(data))
		return
	}
	fmt.Println(buf.String())
}

func printGlobalUsage() {
	fmt.Println("Usage: infrctl <command> [<args>]")
	fmt.Println("Commands:")
	fmt.Println("  project create|list")
	fmt.Println("  service create|list")
	fmt.Println("  domain add")
	fmt.Println("  deploy set")
	fmt.Println("  github repos|register|installations")
	fmt.Println("  builds list|update")
	fmt.Println("")
	fmt.Println("Environment:")
	fmt.Println("  INFRCTL_API   Control plane base URL (default http://localhost:8080)")
	fmt.Println("  INFRCTL_TOKEN Bearer token for authenticated access")
}

func printProjectUsage() {
	fmt.Println("Usage:")
	fmt.Println("  infrctl project create --name <name> [--slug <slug>]")
	fmt.Println("  infrctl project list")
}

func printServiceUsage() {
	fmt.Println("Usage:")
	fmt.Println("  infrctl service create --project <id> --name <name> [--image <ref>] [--port <port>]")
	fmt.Println("  infrctl service list --project <id>")
}

func printDomainUsage() {
	fmt.Println("Usage:")
	fmt.Println("  infrctl domain add --service <id> --hostname <host> [--env <name>]")
}

func printDeployUsage() {
	fmt.Println("Usage:")
	fmt.Println("  infrctl deploy set --service <id> --image <ref> [--env <name>]")
}

func printBuildUsage() {
	fmt.Println("Usage:")
	fmt.Println("  infrctl builds list")
	fmt.Println("  infrctl builds update --id <job-id> [--status pending|running|succeeded|failed] [--reason <text>]")
	fmt.Println("  infrctl builds worker [--name worker-1] [--interval 5s] [--auto-complete=true] [--reason text]")
}

func handleGitHub(client apiClient, args []string) {
    if len(args) == 0 {
        printGitHubUsage()
        os.Exit(1)
    }

    switch args[0] {
    case "repos":
        body, err := client.get("/v1/github/repos")
        if err != nil {
            exitWithError(err)
        }
        printJSON(body)
    case "register":
        fs := flag.NewFlagSet("github register", flag.ExitOnError)
        repo := fs.String("repo", "", "Repository in owner/name form")
        branch := fs.String("branch", "main", "Default branch")
        composePath := fs.String("compose", "docker-compose.yml", "Compose file path")
        installation := fs.String("installation", "", "GitHub App installation ID (optional)")
        serviceID := fs.String("service", "", "Service ID to deploy")
        env := fs.String("env", "production", "Environment name")
        fs.Parse(args[1:])

        owner, name, err := splitRepo(*repo)
        if err != nil {
            fmt.Fprintf(os.Stderr, "invalid --repo: %v\n", err)
            fs.Usage()
            os.Exit(1)
        }

        payload := map[string]string{
            "owner":          owner,
            "name":           name,
            "default_branch": *branch,
            "compose_path":   *composePath,
        }
        if *installation != "" {
            payload["installation_id"] = *installation
        }
        if *serviceID != "" {
            payload["service_id"] = *serviceID
            payload["environment"] = *env
        }

        body, err := client.postJSON("/v1/github/repos", payload)
        if err != nil {
            exitWithError(err)
        }
        printJSON(body)
    case "installations":
        handleGitHubInstallations(client, args[1:])
    default:
        fmt.Fprintf(os.Stderr, "unknown github subcommand: %s\n", args[0])
        printGitHubUsage()
        os.Exit(1)
    }
}

func handleGitHubInstallations(client apiClient, args []string) {
	if len(args) == 0 {
		body, err := client.get("/v1/github/installations")
		if err != nil {
			exitWithError(err)
		}
		printJSON(body)
		return
	}

	switch args[0] {
	case "register":
        fs := flag.NewFlagSet("github installations register", flag.ExitOnError)
        account := fs.String("account", "", "Account login (org/user)")
        external := fs.String("external-id", "", "GitHub installation ID")
        secret := fs.String("secret", "", "Shared webhook secret (optional)")
        fs.Parse(args[1:])

        if *account == "" || *external == "" {
            fmt.Fprintln(os.Stderr, "--account and --external-id are required")
            fs.Usage()
            os.Exit(1)
        }

        payload := map[string]string{
            "account":     *account,
            "external_id": *external,
        }
        if *secret != "" {
            payload["webhook_secret"] = *secret
        }

        body, err := client.postJSON("/v1/github/installations", payload)
        if err != nil {
            exitWithError(err)
        }
        printJSON(body)
	default:
		fmt.Fprintf(os.Stderr, "unknown github installations subcommand: %s\n", args[0])
		fmt.Println("Usage: infrctl github installations [register --account org --external-id 12345]")
		os.Exit(1)
	}
}

func handleBuilds(client apiClient, args []string) {
	if len(args) == 0 {
		printBuildUsage()
		os.Exit(1)
	}

	switch args[0] {
	case "list":
		body, err := client.get("/v1/build-jobs")
		if err != nil {
			exitWithError(err)
		}
		printJSON(body)
	case "update":
		fs := flag.NewFlagSet("builds update", flag.ExitOnError)
		id := fs.String("id", "", "Build job ID")
		status := fs.String("status", "", "New status")
		reason := fs.String("reason", "", "Optional reason")
		fs.Parse(args[1:])

		if *id == "" {
			fmt.Fprintln(os.Stderr, "--id is required")
			fs.Usage()
			os.Exit(1)
		}

		payload := map[string]string{}
		if *status != "" {
			payload["status"] = *status
		}
		if *reason != "" {
			payload["reason"] = *reason
		}

		body, err := client.patchJSON("/v1/build-jobs/"+*id, payload)
		if err != nil {
			exitWithError(err)
		}
		printJSON(body)
	case "worker":
		fs := flag.NewFlagSet("builds worker", flag.ExitOnError)
		name := fs.String("name", "local-worker", "Worker identifier")
		interval := fs.Duration("interval", 5*time.Second, "Polling interval")
		succeed := fs.Bool("auto-complete", true, "Automatically mark jobs succeeded")
		reason := fs.String("reason", "", "Reason to attach on completion")
		fs.Parse(args[1:])

		runBuildWorker(client, *name, *interval, *succeed, *reason)
	default:
		fmt.Fprintf(os.Stderr, "unknown builds subcommand: %s\n", args[0])
		printBuildUsage()
		os.Exit(1)
	}
}

func runBuildWorker(client apiClient, worker string, interval time.Duration, autoComplete bool, completionReason string) {
	fmt.Printf("[worker %s] starting polling loop (interval %s)\n", worker, interval)
	for {
		body, status, err := client.postJSONStatus("/v1/build-jobs/claim", map[string]string{"worker": worker})
		if err != nil {
			fmt.Fprintf(os.Stderr, "[worker %s] claim error: %v\n", worker, err)
			time.Sleep(interval)
			continue
		}
		if status == http.StatusNoContent {
			time.Sleep(interval)
			continue
		}

		var job struct {
			ID           string `json:"id"`
			Repository   string `json:"repository"`
			Ref          string `json:"ref"`
			Commit       string `json:"commit"`
			Installation string `json:"installation"`
		}
		if err := json.Unmarshal(body, &job); err != nil {
			fmt.Fprintf(os.Stderr, "[worker %s] decode claim response: %v\n", worker, err)
			time.Sleep(interval)
			continue
		}

		fmt.Printf("[worker %s] claimed job %s (%s @ %s)\n", worker, job.ID, job.Repository, job.Commit)

		if autoComplete {
			payload := map[string]string{
				"status": "succeeded",
			}
			if completionReason != "" {
				payload["reason"] = completionReason
			}
			if _, err := client.patchJSON("/v1/build-jobs/"+job.ID, payload); err != nil {
				fmt.Fprintf(os.Stderr, "[worker %s] failed to mark job %s: %v\n", worker, job.ID, err)
			} else {
				fmt.Printf("[worker %s] completed job %s\n", worker, job.ID)
			}
		}

		time.Sleep(interval)
	}
}

func splitRepo(repo string) (string, string, error) {
	repo = strings.TrimSpace(repo)
	if repo == "" {
		return "", "", fmt.Errorf("value required")
	}
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("expect owner/name")
	}
	return parts[0], parts[1], nil
}

func printGitHubUsage() {
	fmt.Println("Usage:")
	fmt.Println("  infrctl github repos")
	fmt.Println("  infrctl github register --repo owner/name [--branch main] [--compose docker-compose.yml] [--installation <id>] [--service <service-id>] [--env production]")
	fmt.Println("  infrctl github installations")
	fmt.Println("  infrctl github installations register --account org --external-id 12345 [--secret <value>]")
}

func exitWithError(err error) {
	fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	os.Exit(1)
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
