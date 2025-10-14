package models

import "time"

// Project represents a logical application grouping.
type Project struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"created_at"`
}

// Service represents a deployable unit within a project.
type Service struct {
	ID           string       `json:"id"`
	ProjectID    string       `json:"project_id"`
	Name         string       `json:"name"`
	Image        string       `json:"image"`
	InternalPort int          `json:"internal_port"`
	Compose      string       `json:"compose"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
	Domains      []Domain     `json:"domains"`
	Deployments  []Deployment `json:"deployments"`
}

// Domain ties a hostname to a service in an environment.
type Domain struct {
	ID          string    `json:"id"`
	ServiceID   string    `json:"service_id"`
	Environment string    `json:"environment"`
	Hostname    string    `json:"hostname"`
	CreatedAt   time.Time `json:"created_at"`
}

// Deployment expresses desired image in a specific environment.
type Deployment struct {
	ID          string    `json:"id"`
	ServiceID   string    `json:"service_id"`
	Environment string    `json:"environment"`
	Image       string    `json:"image"`
	CreatedAt   time.Time `json:"created_at"`
}

// Repository represents a GitHub repository linked to the platform.
type Repository struct {
	ID            string    `json:"id"`
	Owner         string    `json:"owner"`
	Name          string    `json:"name"`
	DefaultBranch string    `json:"default_branch"`
	ComposePath   string    `json:"compose_path"`
	Installation  string    `json:"installation_id"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Installation tracks a GitHub App installation that grants access to repositories.
type Installation struct {
	ID            string    `json:"id"`
	Account       string    `json:"account"`
	ExternalID    string    `json:"external_id"`
	WebhookSecret string    `json:"webhook_secret"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// BuildJob represents pending build/deploy work triggered by repo events.
type BuildJob struct {
	ID           string    `json:"id"`
	Repository   string    `json:"repository"`   // owner/name
	Ref          string    `json:"ref"`
	Commit       string    `json:"commit"`
	Installation string    `json:"installation"` // installation external id
	Status       string    `json:"status"`       // pending, running, succeeded, failed
	Reason       string    `json:"reason"`
	WorkerID     string    `json:"worker_id"`
	StartedAt    time.Time `json:"started_at"`
	CompletedAt  time.Time `json:"completed_at"`
	Artifacts    []string  `json:"artifacts"`
	ServiceID    string    `json:"service_id"`
	Environment  string    `json:"environment"`
	ComposePath  string    `json:"compose_path"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
