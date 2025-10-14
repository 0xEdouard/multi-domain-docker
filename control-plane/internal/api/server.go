package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/0xEdouard/multi-domain-infra/control-plane/internal/models"
	"github.com/0xEdouard/multi-domain-infra/control-plane/internal/store"
)

// Server exposes HTTP handlers for the control plane API.
type Server struct {
	store      *store.Store
	apiToken   string
	leResolver string
}

// Config defines initialization values for Server.
type Config struct {
	Store      *store.Store
	APIToken   string
	LEResolver string
}

// New constructs a Server.
func New(cfg Config) *Server {
	return &Server{
		store:      cfg.Store,
		apiToken:   cfg.APIToken,
		leResolver: cfg.LEResolver,
	}
}

// Handler returns the HTTP handler for muxing routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/v1/projects", s.requireAuth(s.handleProjects))
	mux.HandleFunc("/v1/projects/", s.requireAuth(s.handleProjectSubroutes))
	mux.HandleFunc("/v1/services/", s.requireAuth(s.handleServiceSubroutes))
	mux.HandleFunc("/v1/github/repos", s.requireAuth(s.handleRepositories))
	mux.HandleFunc("/v1/github/installations", s.requireAuth(s.handleInstallations))
	mux.HandleFunc("/v1/github/webhook", s.handleGitHubWebhook)
	mux.HandleFunc("/v1/service-compose/", s.requireAuth(s.handleServiceCompose))
	mux.HandleFunc("/v1/state/services", s.requireAuth(s.handleServiceState))
	mux.HandleFunc("/v1/build-jobs/claim", s.requireAuth(s.handleBuildJobClaim))
	mux.HandleFunc("/v1/build-jobs", s.requireAuth(s.handleBuildJobs))
	mux.HandleFunc("/v1/build-jobs/", s.requireAuth(s.handleBuildJob))
	mux.HandleFunc("/v1/traefik/config", s.requireAuth(s.handleTraefikConfig))
	return s.withJSON(mux)
}

func (s *Server) withJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v1/traefik/config") {
			w.Header().Set("Content-Type", "application/json")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.apiToken == "" {
			next(w, r)
			return
		}

		auth := r.Header.Get("Authorization")
		if auth != "Bearer "+s.apiToken {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *Server) handleProjects(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listProjects(w, r)
	case http.MethodPost:
		s.createProject(w, r)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.store.ListProjects()
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"projects": projects})
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if payload.Name == "" {
		http.Error(w, `{"error":"name required"}`, http.StatusBadRequest)
		return
	}
	if payload.Slug == "" {
		payload.Slug = slugify(payload.Name)
	}

	project := &models.Project{
		ID:   newID(),
		Name: payload.Name,
		Slug: payload.Slug,
	}

	if err := s.store.CreateProject(project); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(project)
}

func (s *Server) handleProjectSubroutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/projects/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	projectID := parts[0]

	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			s.getProject(w, r, projectID)
		default:
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		}
		return
	}

	switch parts[1] {
	case "services":
		s.handleProjectServices(w, r, projectID)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) getProject(w http.ResponseWriter, r *http.Request, projectID string) {
	project, err := s.store.GetProject(projectID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, `{"error":"project not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	services, err := s.store.ListServicesByProject(projectID)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(map[string]any{
		"project":  project,
		"services": services,
	})
}

func (s *Server) handleProjectServices(w http.ResponseWriter, r *http.Request, projectID string) {
	switch r.Method {
	case http.MethodGet:
		services, err := s.store.ListServicesByProject(projectID)
		if err != nil {
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"services": services})
	case http.MethodPost:
		s.createService(w, r, projectID)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleRepositories(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		repos, err := s.store.ListRepositories()
		if err != nil {
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"repositories": repos})
	case http.MethodPost:
		var payload struct {
			Owner         string `json:"owner"`
			Name          string `json:"name"`
			DefaultBranch string `json:"default_branch"`
			ComposePath   string `json:"compose_path"`
			Installation  string `json:"installation_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
			return
		}
		payload.Owner = strings.TrimSpace(payload.Owner)
		payload.Name = strings.TrimSpace(payload.Name)
		if payload.Owner == "" || payload.Name == "" {
			http.Error(w, `{"error":"owner and name required"}`, http.StatusBadRequest)
			return
		}
		if payload.DefaultBranch == "" {
			payload.DefaultBranch = "main"
		}
		if payload.ComposePath == "" {
			payload.ComposePath = "docker-compose.yml"
		}

		repo := &models.Repository{
			ID:            repositoryID(payload.Owner, payload.Name),
			Owner:         payload.Owner,
			Name:          payload.Name,
			DefaultBranch: payload.DefaultBranch,
			ComposePath:   payload.ComposePath,
			Installation:  payload.Installation,
		}

		if err := s.store.UpsertRepository(repo); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(repo)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleInstallations(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		installations, err := s.store.ListInstallations()
		if err != nil {
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"installations": installations})
	case http.MethodPost:
		var payload struct {
			Account       string `json:"account"`
			ExternalID    string `json:"external_id"`
			WebhookSecret string `json:"webhook_secret"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
			return
		}
		payload.Account = strings.TrimSpace(payload.Account)
		payload.ExternalID = strings.TrimSpace(payload.ExternalID)
		if payload.Account == "" || payload.ExternalID == "" {
			http.Error(w, `{"error":"account and external_id required"}`, http.StatusBadRequest)
			return
		}

		inst := &models.Installation{
			ID:            installationID(payload.Account, payload.ExternalID),
			Account:       payload.Account,
			ExternalID:    payload.ExternalID,
			WebhookSecret: payload.WebhookSecret,
		}

		if err := s.store.UpsertInstallation(inst); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(inst)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (s *Server) createService(w http.ResponseWriter, r *http.Request, projectID string) {
	var payload struct {
		Name         string `json:"name"`
		Image        string `json:"image"`
		InternalPort int    `json:"internal_port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	if payload.Name == "" {
		http.Error(w, `{"error":"name required"}`, http.StatusBadRequest)
		return
	}
	if payload.InternalPort == 0 {
		payload.InternalPort = 80
	}

	service := &models.Service{
		ID:           newID(),
		ProjectID:    projectID,
		Name:         payload.Name,
		Image:        payload.Image,
		InternalPort: payload.InternalPort,
		Domains:      []models.Domain{},
		Deployments:  []models.Deployment{},
	}

	if err := s.store.CreateService(service); err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(service)
}

func (s *Server) handleServiceSubroutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/services/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	serviceID := parts[0]

	if len(parts) == 1 {
		switch r.Method {
		case http.MethodGet:
			s.getService(w, r, serviceID)
		default:
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		}
		return
	}

	switch parts[1] {
	case "domains":
		s.handleServiceDomains(w, r, serviceID)
	case "deployments":
		s.handleServiceDeployments(w, r, serviceID)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleServiceCompose(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/v1/service-compose/")
	parts := strings.Split(path, "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	serviceID := parts[0]
	service, err := s.store.GetService(serviceID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, `{"error":"service not found"}`, http.StatusNotFound)
		} else {
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		}
		return
	}

	switch r.Method {
	case http.MethodGet:
		_ = json.NewEncoder(w).Encode(map[string]string{"compose": service.Compose})
	case http.MethodPut, http.MethodPost:
		var payload struct {
			Compose string `json:"compose"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
			return
		}
		service.Compose = payload.Compose
		if err := s.store.UpdateService(service); err != nil {
			http.Error(w, `{"error":"failed to update"}`, http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleServiceState(w http.ResponseWriter, r *http.Request) {
	projects, err := s.store.ListProjects()
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	type serviceInfo struct {
		ID           string `json:"id"`
		ProjectID    string `json:"project_id"`
		Name         string `json:"name"`
		Image        string `json:"image"`
		InternalPort int    `json:"internal_port"`
		Compose      string `json:"compose"`
	}

	var services []serviceInfo
	for _, project := range projects {
		list, err := s.store.ListServicesByProject(project.ID)
		if err != nil {
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		for _, svc := range list {
			services = append(services, serviceInfo{
				ID:           svc.ID,
				ProjectID:    svc.ProjectID,
				Name:         svc.Name,
				Image:        svc.Image,
				InternalPort: svc.InternalPort,
				Compose:      svc.Compose,
			})
		}
	}

	_ = json.NewEncoder(w).Encode(map[string]any{"services": services})
}

func (s *Server) getService(w http.ResponseWriter, r *http.Request, serviceID string) {
	service, err := s.store.GetService(serviceID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, `{"error":"service not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(service)
}

func (s *Server) handleServiceDomains(w http.ResponseWriter, r *http.Request, serviceID string) {
	service, err := s.store.GetService(serviceID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, `{"error":"service not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	switch r.Method {
	case http.MethodGet:
		_ = json.NewEncoder(w).Encode(map[string]any{"domains": service.Domains})
	case http.MethodPost:
		var payload struct {
			Environment string `json:"environment"`
			Hostname    string `json:"hostname"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
			return
		}
		if payload.Environment == "" {
			payload.Environment = "production"
		}
		if payload.Hostname == "" {
			http.Error(w, `{"error":"hostname required"}`, http.StatusBadRequest)
			return
		}

		domain := models.Domain{
			ID:          newID(),
			ServiceID:   serviceID,
			Environment: payload.Environment,
			Hostname:    payload.Hostname,
			CreatedAt:   time.Now().UTC(),
		}

		service.Domains = append(service.Domains, domain)
		if err := s.store.UpdateService(service); err != nil {
			http.Error(w, `{"error":"failed to persist domain"}`, http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(domain)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleServiceDeployments(w http.ResponseWriter, r *http.Request, serviceID string) {
	service, err := s.store.GetService(serviceID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			http.Error(w, `{"error":"service not found"}`, http.StatusNotFound)
			return
		}
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	switch r.Method {
	case http.MethodGet:
		_ = json.NewEncoder(w).Encode(map[string]any{"deployments": service.Deployments})
	case http.MethodPost:
		var payload struct {
			Environment string `json:"environment"`
			Image       string `json:"image"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
			return
		}
		if payload.Environment == "" {
			payload.Environment = "production"
		}
		if payload.Image == "" {
			http.Error(w, `{"error":"image required"}`, http.StatusBadRequest)
			return
		}

		deployment := models.Deployment{
			ID:          newID(),
			ServiceID:   serviceID,
			Environment: payload.Environment,
			Image:       payload.Image,
			CreatedAt:   time.Now().UTC(),
		}

		replaced := false
		for idx, d := range service.Deployments {
			if d.Environment == payload.Environment {
				service.Deployments[idx] = deployment
				replaced = true
				break
			}
		}
		if !replaced {
			service.Deployments = append(service.Deployments, deployment)
		}

		service.Image = payload.Image
		if err := s.store.UpdateService(service); err != nil {
			http.Error(w, `{"error":"failed to persist deployment"}`, http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(deployment)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleTraefikConfig(w http.ResponseWriter, r *http.Request) {
	projects, err := s.store.ListProjects()
	if err != nil {
		http.Error(w, `internal error`, http.StatusInternalServerError)
		return
	}

	var services []*models.Service
	for _, project := range projects {
		svcList, err := s.store.ListServicesByProject(project.ID)
		if err != nil {
			http.Error(w, `internal error`, http.StatusInternalServerError)
			return
		}
		services = append(services, svcList...)
	}

	w.Header().Set("Content-Type", "application/x-yaml")
	config := renderTraefikConfig(services, s.leResolver)
	_, _ = w.Write([]byte(config))
}

func (s *Server) handleBuildJobs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		jobs, err := s.store.ListBuildJobs()
		if err != nil {
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"build_jobs": jobs})
	case http.MethodPost:
		var payload struct {
			Repository   string   `json:"repository"`
			Ref          string   `json:"ref"`
			Commit       string   `json:"commit"`
			Installation string   `json:"installation"`
			Status       string   `json:"status"`
			ServiceID    string   `json:"service_id"`
			Environment  string   `json:"environment"`
			Artifacts    []string `json:"artifacts"`
			ComposePath  string   `json:"compose_path"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
			return
		}
		if payload.Repository == "" || payload.Commit == "" {
			http.Error(w, `{"error":"repository and commit required"}`, http.StatusBadRequest)
			return
		}
		job := &models.BuildJob{
			ID:           newID(),
			Repository:   payload.Repository,
			Ref:          payload.Ref,
			Commit:       payload.Commit,
			Installation: payload.Installation,
			Status:       payload.Status,
			ServiceID:    payload.ServiceID,
			Environment:  payload.Environment,
			Artifacts:    payload.Artifacts,
			ComposePath:  payload.ComposePath,
		}
		if err := s.store.CreateBuildJob(job); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"%s"}`, err), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(job)
	default:
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleBuildJob(w http.ResponseWriter, r *http.Request) {
    if !strings.HasPrefix(r.URL.Path, "/v1/build-jobs/") {
        http.NotFound(w, r)
        return
    }
    id := strings.TrimPrefix(r.URL.Path, "/v1/build-jobs/")
    if id == "" {
        http.NotFound(w, r)
        return
    }

    switch r.Method {
    case http.MethodGet:
        job, err := s.store.GetBuildJob(id)
        if err != nil {
            if errors.Is(err, store.ErrNotFound) {
                http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
                return
            }
            http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
            return
        }
        _ = json.NewEncoder(w).Encode(job)
    case http.MethodPatch, http.MethodPost:
        job, err := s.store.GetBuildJob(id)
        if err != nil {
            if errors.Is(err, store.ErrNotFound) {
                http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
                return
            }
            http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
            return
        }
        var payload struct {
            Status      string   `json:"status"`
            Reason      string   `json:"reason"`
            Artifacts   []string `json:"artifacts"`
            ComposePath string   `json:"compose_path"`
        }
        if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
            http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
            return
        }
        if payload.Status != "" {
            job.Status = payload.Status
        }
        if payload.Artifacts != nil {
            job.Artifacts = payload.Artifacts
        }
        if payload.ComposePath != "" {
            job.ComposePath = payload.ComposePath
        }
        job.Reason = payload.Reason
        if err := s.store.UpdateBuildJob(job); err != nil {
            http.Error(w, `{"error":"failed to update"}`, http.StatusInternalServerError)
            return
        }
        _ = json.NewEncoder(w).Encode(job)
    default:
        http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
    }
}

func (s *Server) handleBuildJobClaim(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	var payload struct {
		Worker string `json:"worker"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil && err != io.EOF {
		http.Error(w, `{"error":"invalid json"}`, http.StatusBadRequest)
		return
	}
	worker := payload.Worker
	if worker == "" {
		worker = "worker-" + newID()
	}

	job, err := s.store.ClaimNextPendingBuildJob(worker)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}

	_ = json.NewEncoder(w).Encode(job)
}

func (s *Server) handleGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	payload, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, `{"error":"invalid body"}`, http.StatusBadRequest)
		return
	}

	event := r.Header.Get("X-GitHub-Event")
	deliveryID := r.Header.Get("X-GitHub-Delivery")
	installationIDHeader := r.Header.Get("X-GitHub-Installation-Id")

	installationID := installationIDHeader
	if installationID == "" {
		installationID = extractInstallationID(payload)
	}

	var inst *models.Installation
	if installationID != "" {
		installation, err := s.store.FindInstallationByExternalID(installationID)
		if err == nil {
			inst = installation
		} else if err != nil && !errors.Is(err, store.ErrNotFound) {
			log.Printf("[webhook] lookup installation %s failed: %v", installationID, err)
		}
	}

	sig := r.Header.Get("X-Hub-Signature-256")
	if inst != nil && inst.WebhookSecret != "" {
		if err := verifySignature(sig, payload, inst.WebhookSecret); err != nil {
			http.Error(w, fmt.Sprintf(`{"error":"signature invalid: %s"}`, err), http.StatusUnauthorized)
			return
		}
	} else if sig != "" {
		log.Printf("[webhook] signature provided but no secret registered for installation %s", installationID)
	}

	response := map[string]any{
		"status":        "accepted",
		"event":         event,
		"delivery_id":   deliveryID,
		"installation_id": installationID,
	}

	switch event {
	case "push":
		if info, err := parsePushEvent(payload); err == nil {
			response["repository"] = info.Repository
			response["ref"] = info.Ref
			response["commit"] = info.After
			if info.Repository != "" && info.After != "" {
				job := &models.BuildJob{
					ID:           newID(),
					Repository:   info.Repository,
					Ref:          info.Ref,
					Commit:       info.After,
					Installation: installationID,
					Status:       "pending",
				}
				if owner, name, err := splitRepoFullName(info.Repository); err == nil {
					repoID := repositoryID(owner, name)
					repo, repoErr := s.store.GetRepository(repoID)
					if repoErr == nil {
						job.ServiceID = repo.ServiceID
						if repo.Environment != "" {
							job.Environment = repo.Environment
						} else if repo.ServiceID != "" {
							job.Environment = "production"
						}
						job.ComposePath = repo.ComposePath
					} else if repoErr != nil && !errors.Is(repoErr, store.ErrNotFound) {
						log.Printf("[webhook] repository lookup failed: %v", repoErr)
					}
				} else {
					log.Printf("[webhook] invalid repository name %s: %v", info.Repository, err)
				}
				if err := s.store.CreateBuildJob(job); err != nil {
					log.Printf("[webhook] failed to enqueue build job: %v", err)
				} else {
					response["build_job_id"] = job.ID
				}
			}
		} else {
			log.Printf("[webhook] failed to parse push payload: %v", err)
		}
	case "installation_repositories":
		if info, err := parseInstallationReposEvent(payload); err == nil {
			response["action"] = info.Action
			response["repositories"] = info.Existing
			response["added"] = info.Added
			response["removed"] = info.Removed

			// Upsert existing repositories to keep metadata fresh.
			for _, repo := range info.Existing {
				owner, name := resolveRepoOwnerName(repo)
				if owner == "" || name == "" {
					continue
				}
				repoModel := &models.Repository{
					ID:            repositoryID(owner, name),
					Owner:         owner,
					Name:          name,
					DefaultBranch: repo.DefaultBranch,
					ComposePath:   "docker-compose.yml",
					Installation:  installationID,
				}
				if err := s.store.UpsertRepository(repoModel); err != nil {
					log.Printf("[webhook] failed to upsert repository %s/%s: %v", owner, name, err)
				}
			}

			// Ensure added repositories are recorded explicitly.
			for _, repo := range info.Added {
				owner, name := resolveRepoOwnerName(repo)
				if owner == "" || name == "" {
					continue
				}
				repoModel := &models.Repository{
					ID:            repositoryID(owner, name),
					Owner:         owner,
					Name:          name,
					DefaultBranch: repo.DefaultBranch,
					ComposePath:   "docker-compose.yml",
					Installation:  installationID,
				}
				if err := s.store.UpsertRepository(repoModel); err != nil {
					log.Printf("[webhook] failed to register repository %s/%s: %v", owner, name, err)
				}
			}

			// Remove repositories that were deleted from the installation.
			for _, repo := range info.Removed {
				owner, name := resolveRepoOwnerName(repo)
				if owner == "" || name == "" {
					continue
				}
				id := repositoryID(owner, name)
				if err := s.store.DeleteRepository(id); err != nil && !errors.Is(err, store.ErrNotFound) {
					log.Printf("[webhook] failed to delete repository %s/%s: %v", owner, name, err)
				}
			}
		} else {
			log.Printf("[webhook] failed to parse installation_repositories payload: %v", err)
		}
	default:
		log.Printf("[webhook] received %s event (delivery %s)", event, deliveryID)
	}

	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(response)
}

func slugify(input string) string {
	input = strings.TrimSpace(strings.ToLower(input))
	input = strings.ReplaceAll(input, " ", "-")
	input = strings.ReplaceAll(input, "_", "-")
	return input
}

func repositoryID(owner, name string) string {
	return sanitizeKey(owner) + "-" + sanitizeKey(name)
}

func installationID(account, external string) string {
	return sanitizeKey(account) + "-" + sanitizeKey(external)
}

func splitRepoFullName(full string) (string, string, error) {
	parts := strings.Split(full, "/")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid repository name: %s", full)
	}
	owner := strings.TrimSpace(parts[0])
	name := strings.TrimSpace(parts[1])
	if owner == "" || name == "" {
		return "", "", fmt.Errorf("invalid repository name: %s", full)
	}
	return owner, name, nil
}

func resolveRepoOwnerName(info repoInfo) (string, string) {
	owner := info.Owner
	name := info.Name
	if (owner == "" || name == "") && info.FullName != "" {
		parts := strings.Split(info.FullName, "/")
		if len(parts) == 2 {
			if owner == "" {
				owner = parts[0]
			}
			if name == "" {
				name = parts[1]
			}
		}
	}
	return strings.TrimSpace(owner), strings.TrimSpace(name)
}

func extractInstallationID(payload []byte) string {
	var body struct {
		Installation struct {
			ID int64 `json:"id"`
		} `json:"installation"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return ""
	}
	if body.Installation.ID == 0 {
		return ""
	}
	return strconv.FormatInt(body.Installation.ID, 10)
}

func verifySignature(signatureHeader string, payload []byte, secret string) error {
	const prefix = "sha256="
	if signatureHeader == "" {
		return errors.New("missing signature header")
	}
	if !strings.HasPrefix(signatureHeader, prefix) {
		return fmt.Errorf("unexpected signature format")
	}
	sigBytes, err := hex.DecodeString(signatureHeader[len(prefix):])
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := mac.Sum(nil)
	if !hmac.Equal(expected, sigBytes) {
		return errors.New("signature mismatch")
	}
	return nil
}

type pushEventInfo struct {
	Repository string
	Ref        string
	After      string
}

func parsePushEvent(payload []byte) (pushEventInfo, error) {
	var body struct {
		Ref  string `json:"ref"`
		After string `json:"after"`
		Repository struct {
			FullName string `json:"full_name"`
			Name     string `json:"name"`
			Owner    struct {
				Login string `json:"login"`
			} `json:"owner"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return pushEventInfo{}, err
	}
	repo := body.Repository.FullName
	if repo == "" && body.Repository.Owner.Login != "" && body.Repository.Name != "" {
		repo = body.Repository.Owner.Login + "/" + body.Repository.Name
	}
	return pushEventInfo{
		Repository: repo,
		Ref:        body.Ref,
		After:      body.After,
	}, nil
}

type repoInfo struct {
	FullName     string
	Owner        string
	Name         string
	DefaultBranch string
}

type installationReposInfo struct {
    Action   string
    Added    []repoInfo
    Removed  []repoInfo
    Existing []repoInfo
}

func resolveRepoOwnerName(info repoInfo) (string, string) {
	owner := strings.TrimSpace(info.Owner)
	name := strings.TrimSpace(info.Name)
	if owner == "" || name == "" {
		if info.FullName != "" {
			parts := strings.Split(info.FullName, "/")
			if len(parts) == 2 {
				if owner == "" {
					owner = strings.TrimSpace(parts[0])
				}
				if name == "" {
					name = strings.TrimSpace(parts[1])
				}
			}
		}
	}
	return owner, name
}

func parseInstallationReposEvent(payload []byte) (installationReposInfo, error) {
    var body struct {
        Action string `json:"action"`
        Repositories []struct {
            FullName      string `json:"full_name"`
            Name          string `json:"name"`
            DefaultBranch string `json:"default_branch"`
            Owner         struct {
                Login string `json:"login"`
            } `json:"owner"`
        } `json:"repositories"`
        RepositoriesAdded []struct {
            FullName      string `json:"full_name"`
            Name          string `json:"name"`
            DefaultBranch string `json:"default_branch"`
            Owner         struct {
                Login string `json:"login"`
            } `json:"owner"`
        } `json:"repositories_added"`
        RepositoriesRemoved []struct {
            FullName      string `json:"full_name"`
            Name          string `json:"name"`
            DefaultBranch string `json:"default_branch"`
            Owner         struct {
                Login string `json:"login"`
            } `json:"owner"`
        } `json:"repositories_removed"`
    }
    if err := json.Unmarshal(payload, &body); err != nil {
        return installationReposInfo{}, err
    }

    conv := func(full, owner, name, branch string) repoInfo {
        if full == "" && owner != "" && name != "" {
            full = owner + "/" + name
        }
        if branch == "" {
            branch = "main"
        }
        return repoInfo{
            FullName: full,
            Owner:    owner,
            Name:     name,
            DefaultBranch: branch,
        }
    }

    info := installationReposInfo{Action: body.Action}

    for _, repo := range body.Repositories {
        info.Existing = append(info.Existing, conv(repo.FullName, repo.Owner.Login, repo.Name, repo.DefaultBranch))
    }
    for _, repo := range body.RepositoriesAdded {
        info.Added = append(info.Added, conv(repo.FullName, repo.Owner.Login, repo.Name, repo.DefaultBranch))
    }
    for _, repo := range body.RepositoriesRemoved {
        info.Removed = append(info.Removed, conv(repo.FullName, repo.Owner.Login, repo.Name, repo.DefaultBranch))
    }

    return info, nil
}
