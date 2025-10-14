package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/0xEdouard/multi-domain-infra/control-plane/internal/models"
)

// ErrNotFound represents missing records.
var ErrNotFound = errors.New("store: not found")

// State contains persisted data.
type State struct {
	Projects map[string]*models.Project `json:"projects"`
	Services map[string]*models.Service `json:"services"`
	Repos    map[string]*models.Repository `json:"repos"`
	Installations map[string]*models.Installation `json:"installations"`
	BuildJobs map[string]*models.BuildJob `json:"build_jobs"`
}

// Store provides synchronized access to state.
type Store struct {
	path string
	mu   sync.RWMutex
	data State
}

// New instantiates a Store backed by a JSON file.
func New(path string) (*Store, error) {
	s := &Store{
		path: path,
		data: State{
			Projects: make(map[string]*models.Project),
			Services: make(map[string]*models.Service),
			Repos:    make(map[string]*models.Repository),
			Installations: make(map[string]*models.Installation),
			BuildJobs: make(map[string]*models.BuildJob),
		},
	}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}

	bytes, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s.persistLocked()
		}
		return err
	}
	if len(bytes) == 0 {
		return nil
	}

	var state State
	if err := json.Unmarshal(bytes, &state); err != nil {
		return err
	}
	if state.Projects == nil {
		state.Projects = make(map[string]*models.Project)
	}
	if state.Services == nil {
		state.Services = make(map[string]*models.Service)
	}
	if state.Repos == nil {
		state.Repos = make(map[string]*models.Repository)
	}
	if state.Installations == nil {
		state.Installations = make(map[string]*models.Installation)
	}
	if state.BuildJobs == nil {
		state.BuildJobs = make(map[string]*models.BuildJob)
	}
	s.data = state
	return nil
}

func (s *Store) persistLocked() error {
	bytes, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, bytes, 0600)
}

// ListProjects returns all projects.
func (s *Store) ListProjects() ([]*models.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*models.Project, 0, len(s.data.Projects))
	for _, p := range s.data.Projects {
		result = append(result, cloneProject(p))
	}
	return result, nil
}

// CreateProject stores a new project.
func (s *Store) CreateProject(p *models.Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.data.Projects[p.ID]; ok {
		return errors.New("store: project already exists")
	}
	now := time.Now().UTC()
	p.CreatedAt = now
	s.data.Projects[p.ID] = cloneProject(p)
	return s.persistLocked()
}

// GetProject fetches a project by ID.
func (s *Store) GetProject(id string) (*models.Project, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	project, ok := s.data.Projects[id]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneProject(project), nil
}

// ListServicesByProject returns services associated to a project.
func (s *Store) ListServicesByProject(projectID string) ([]*models.Service, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var services []*models.Service
	for _, svc := range s.data.Services {
		if svc.ProjectID == projectID {
			services = append(services, cloneService(svc))
		}
	}
	return services, nil
}

// CreateService stores a new service.
func (s *Store) CreateService(service *models.Service) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.data.Services[service.ID]; ok {
		return errors.New("store: service already exists")
	}

	now := time.Now().UTC()
	service.CreatedAt = now
	service.UpdatedAt = now
	s.data.Services[service.ID] = cloneService(service)
	return s.persistLocked()
}

// UpdateService persists updates to an existing service.
func (s *Store) UpdateService(service *models.Service) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.data.Services[service.ID]; !ok {
		return ErrNotFound
	}

	service.UpdatedAt = time.Now().UTC()
	s.data.Services[service.ID] = cloneService(service)
	return s.persistLocked()
}

// GetService fetches a service by ID.
func (s *Store) GetService(id string) (*models.Service, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	service, ok := s.data.Services[id]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneService(service), nil
}

func cloneProject(p *models.Project) *models.Project {
	if p == nil {
		return nil
	}
	copy := *p
	return &copy
}

func cloneService(svc *models.Service) *models.Service {
	if svc == nil {
		return nil
	}
	copy := *svc
	copy.Compose = svc.Compose
	if svc.Domains != nil {
		copy.Domains = append([]models.Domain(nil), svc.Domains...)
	}
	if svc.Deployments != nil {
		copy.Deployments = append([]models.Deployment(nil), svc.Deployments...)
	}
	return &copy
}

// ListRepositories returns all registered repositories.
func (s *Store) ListRepositories() ([]*models.Repository, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*models.Repository, 0, len(s.data.Repos))
	for _, repo := range s.data.Repos {
		result = append(result, cloneRepository(repo))
	}
	return result, nil
}

// GetRepository returns a repository by ID.
func (s *Store) GetRepository(id string) (*models.Repository, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	repo, ok := s.data.Repos[id]
	if !ok {
		return nil, ErrNotFound
	}
	return cloneRepository(repo), nil
}

// UpsertRepository inserts or updates repository metadata.
func (s *Store) UpsertRepository(repo *models.Repository) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	if existing, ok := s.data.Repos[repo.ID]; ok {
		repo.CreatedAt = existing.CreatedAt
		if repo.ServiceID == "" {
			repo.ServiceID = existing.ServiceID
		}
		if repo.Environment == "" {
			repo.Environment = existing.Environment
		}
		if repo.ComposePath == "" {
			repo.ComposePath = existing.ComposePath
		}
		if existing.Installation != "" && repo.Installation == "" {
			repo.Installation = existing.Installation
		}
	} else {
		repo.CreatedAt = now
	}
	repo.UpdatedAt = now
	s.data.Repos[repo.ID] = cloneRepository(repo)
	return s.persistLocked()
}

func cloneRepository(repo *models.Repository) *models.Repository {
	if repo == nil {
		return nil
	}
	copy := *repo
	return &copy
}

// DeleteRepository removes a repository record.
func (s *Store) DeleteRepository(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.data.Repos[id]; !ok {
		return ErrNotFound
	}
	delete(s.data.Repos, id)
	return s.persistLocked()
}

// ListInstallations returns recorded GitHub App installations.
func (s *Store) ListInstallations() ([]*models.Installation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*models.Installation, 0, len(s.data.Installations))
	for _, inst := range s.data.Installations {
		result = append(result, cloneInstallation(inst))
	}
	return result, nil
}

// UpsertInstallation stores installation details.
func (s *Store) UpsertInstallation(inst *models.Installation) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	if existing, ok := s.data.Installations[inst.ID]; ok {
		inst.CreatedAt = existing.CreatedAt
		if inst.WebhookSecret == "" {
			inst.WebhookSecret = existing.WebhookSecret
		}
	} else {
		inst.CreatedAt = now
	}
	inst.UpdatedAt = now
	s.data.Installations[inst.ID] = cloneInstallation(inst)
	return s.persistLocked()
}

func cloneInstallation(inst *models.Installation) *models.Installation {
	if inst == nil {
		return nil
	}
	copy := *inst
	return &copy
}

// FindInstallationByExternalID retrieves an installation by external ID.
func (s *Store) FindInstallationByExternalID(externalID string) (*models.Installation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, inst := range s.data.Installations {
		if inst.ExternalID == externalID {
			return cloneInstallation(inst), nil
		}
	}
	return nil, ErrNotFound
}

// CreateBuildJob stores a new build job.
func (s *Store) CreateBuildJob(job *models.BuildJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if job.Status == "" {
		job.Status = "pending"
	}
	if job.ServiceID != "" && job.Environment == "" {
		job.Environment = "production"
	}
	now := time.Now().UTC()
	job.CreatedAt = now
	job.UpdatedAt = now
	job.StartedAt = time.Time{}
	job.CompletedAt = time.Time{}
	job.WorkerID = ""
	job.Artifacts = append([]string(nil), job.Artifacts...)
	job.ComposePath = strings.TrimSpace(job.ComposePath)
	s.data.BuildJobs[job.ID] = cloneBuildJob(job)
	return s.persistLocked()
}

// ListBuildJobs returns build jobs sorted by creation order.
func (s *Store) ListBuildJobs() ([]*models.BuildJob, error) {
    s.mu.RLock()
    defer s.mu.RUnlock()

    jobs := make([]*models.BuildJob, 0, len(s.data.BuildJobs))
    for _, job := range s.data.BuildJobs {
        jobs = append(jobs, cloneBuildJob(job))
    }
    sort.Slice(jobs, func(i, j int) bool {
        return jobs[i].CreatedAt.Before(jobs[j].CreatedAt)
    })
    return jobs, nil
}

// UpdateBuildJob updates status/reason for a job.
func (s *Store) UpdateBuildJob(job *models.BuildJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.data.BuildJobs[job.ID]; !ok {
		return ErrNotFound
	}
	job.UpdatedAt = time.Now().UTC()
	if job.Status == "succeeded" || job.Status == "failed" {
		if job.CompletedAt.IsZero() {
			job.CompletedAt = job.UpdatedAt
		}
	} else if !job.CompletedAt.IsZero() {
		job.CompletedAt = time.Time{}
	}
	job.Artifacts = append([]string(nil), job.Artifacts...)
	job.ComposePath = strings.TrimSpace(job.ComposePath)
	s.data.BuildJobs[job.ID] = cloneBuildJob(job)
	return s.persistLocked()
}

func cloneBuildJob(job *models.BuildJob) *models.BuildJob {
	if job == nil {
		return nil
	}
	copy := *job
	if job.Artifacts != nil {
		copy.Artifacts = append([]string(nil), job.Artifacts...)
	}
	return &copy
}

// ClaimNextPendingBuildJob marks the oldest pending job as running and returns it.
func (s *Store) ClaimNextPendingBuildJob(workerID string) (*models.BuildJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var selectedID string
	var selectedJob *models.BuildJob

	for id, job := range s.data.BuildJobs {
		if job.Status != "pending" {
			continue
		}
		if selectedJob == nil || job.CreatedAt.Before(selectedJob.CreatedAt) {
			copy := *job
			selectedJob = &copy
			selectedID = id
		}
	}

	if selectedJob == nil {
		return nil, ErrNotFound
	}

	job := s.data.BuildJobs[selectedID]
	job.Status = "running"
	job.WorkerID = workerID
	job.StartedAt = time.Now().UTC()
	job.UpdatedAt = job.StartedAt
	s.data.BuildJobs[selectedID] = cloneBuildJob(job)

	if err := s.persistLocked(); err != nil {
		return nil, err
	}

	return cloneBuildJob(job), nil
}

// GetBuildJob returns a build job by ID.
func (s *Store) GetBuildJob(id string) (*models.BuildJob, error) {
    s.mu.RLock()
    defer s.mu.RUnlock()

    job, ok := s.data.BuildJobs[id]
    if !ok {
        return nil, ErrNotFound
    }
    return cloneBuildJob(job), nil
}
