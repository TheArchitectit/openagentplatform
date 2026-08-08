package team

import (
	"sync"
)

// TeamMember represents a person assigned to a team role

type TeamMember struct {
	Person     string `json:"person"`
	Role       string `json:"role"`
	AssignedAt string `json:"assigned_at"`
}

// TeamAssignment represents a role assignment
type TeamAssignment struct {
	TeamID     int    `json:"team_id"`
	RoleName   string `json:"role_name"`
	Person     string `json:"person"`
	AssignedAt string `json:"assigned_at"`
}

// PhaseStatus represents the status of a phase
type PhaseStatus struct {
	Phase       string  `json:"phase"`
	TotalTeams  int     `json:"total_teams"`
	Completed   int     `json:"completed"`
	Active      int     `json:"active"`
	NotStarted  int     `json:"not_started"`
	ProgressPct float64 `json:"progress_pct"`
}

// TeamListItem represents a team in list output
type TeamListItem struct {
	ID             int    `json:"id"`
	Name           string `json:"name"`
	Phase          string `json:"phase"`
	Status         string `json:"status"`
	AssignedCount  int    `json:"assigned_count"`
	TotalRoles     int    `json:"total_roles"`
}

// Manager handles team operations with thread-safe access
type Manager struct {
	projectName string
	baseDir     string
	configPath  string
	lockPath    string
	teams       map[int]Team
	mu          sync.RWMutex
}

// ManagerOption configures the Manager
