package models

import (
	"encoding/json"
	"fmt"
	"time"
)

// ============================================================
// TaskStatus constants (mirrors proto enum TaskStatus)
// ============================================================

const (
	TaskStatusUnspecified      = "pending" // wire default maps to pending
	TaskStatusPending          = "pending"
	TaskStatusWorking          = "working"
	TaskStatusInputRequired    = "input-required"
	TaskStatusOutputAvailable  = "output-available"
	TaskStatusCompleted        = "completed"
	TaskStatusFailed           = "failed"
	TaskStatusCancelled        = "cancelled"
)

// ValidTaskStatuses lists all valid TaskStatus string values.
var ValidTaskStatuses = map[string]bool{
	TaskStatusPending:         true,
	TaskStatusWorking:         true,
	TaskStatusInputRequired:   true,
	TaskStatusOutputAvailable: true,
	TaskStatusCompleted:       true,
	TaskStatusFailed:          true,
	TaskStatusCancelled:       true,
}

// IsTerminal returns true if the status is a terminal state.
func IsTerminal(status string) bool {
	switch status {
	case TaskStatusCompleted, TaskStatusFailed, TaskStatusCancelled:
		return true
	}
	return false
}

// ============================================================
// Domain types
// ============================================================

type AgentCard struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	Description    string       `json:"description"`
	Version        string       `json:"version"`
	Framework      string       `json:"framework"`
	Endpoint       string       `json:"endpoint"`
	Capabilities   []string     `json:"capabilities"`
	Tags           []string     `json:"tags"`
	Skills         []Skill      `json:"skills"`
	Authentication *AuthScheme  `json:"authentication,omitempty"`
}

type Skill struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
}

type AuthScheme struct {
	Type   string            `json:"type"`
	Config map[string]string `json:"config"`
}

type Message struct {
	ID    string `json:"id"`
	Role  string `json:"role"`
	Parts []Part `json:"parts"`
}

type Part struct {
	Text string    `json:"text,omitempty"`
	File *FileRef  `json:"file,omitempty"`
	Data []byte    `json:"data,omitempty"`
}

type FileRef struct {
	Name     string `json:"name"`
	MimeType string `json:"mime_type"`
	URI      string `json:"uri"`
}

type Artifact struct {
	ID          string    `json:"id"`
	TaskID      string    `json:"task_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Parts       []Part    `json:"parts"`
	MimeType    string    `json:"mime_type"`
	CreatedAt   time.Time `json:"created_at"`
}

type Task struct {
	ID         string            `json:"id"`
	ContextID  string            `json:"context_id"`
	AgentID    string            `json:"agent_id"`
	Status     string            `json:"status"`
	Message    Message           `json:"message"`
	Artifacts  []Artifact        `json:"artifacts"`
	Metadata   map[string]string `json:"metadata"`
	Version    int32             `json:"version"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

type TaskStatusUpdate struct {
	TaskID    string    `json:"task_id"`
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ============================================================
// Validation methods
// ============================================================

func (c *AgentCard) Validate() error {
	if c.ID == "" {
		return fmt.Errorf("agent card: id is required")
	}
	if c.Name == "" {
		return fmt.Errorf("agent card: name is required")
	}
	if c.Endpoint == "" {
		return fmt.Errorf("agent card: endpoint is required")
	}
	for i, s := range c.Skills {
		if err := s.Validate(); err != nil {
			return fmt.Errorf("agent card: skill[%d]: %w", i, err)
		}
	}
	return nil
}

func (s *Skill) Validate() error {
	if s.ID == "" {
		return fmt.Errorf("skill: id is required")
	}
	if s.Name == "" {
		return fmt.Errorf("skill: name is required")
	}
	return nil
}

func (t *Task) Validate() error {
	if t.ID == "" {
		return fmt.Errorf("task: id is required")
	}
	if t.AgentID == "" {
		return fmt.Errorf("task: agent_id is required")
	}
	if !ValidTaskStatuses[t.Status] {
		return fmt.Errorf("task: invalid status %q", t.Status)
	}
	return nil
}

func (p *Part) Validate() error {
	if p.Text == "" && p.File == nil && len(p.Data) == 0 {
		return fmt.Errorf("part: must have text, file, or data content")
	}
	return nil
}

func (m *Message) Validate() error {
	if m.Role == "" {
		return fmt.Errorf("message: role is required")
	}
	if len(m.Parts) == 0 {
		return fmt.Errorf("message: at least one part is required")
	}
	for i, p := range m.Parts {
		if err := p.Validate(); err != nil {
			return fmt.Errorf("message: part[%d]: %w", i, err)
		}
	}
	return nil
}

// ============================================================
// Conversion functions (domain <-> wire/JSON serialization helpers)
// ============================================================

// TaskToJSON serializes a Task to JSON bytes suitable for storage in jsonb columns.
func TaskToJSON(t *Task) ([]byte, error) {
	return json.Marshal(t)
}

// TaskFromJSON deserializes a Task from JSON bytes (jsonb column).
func TaskFromJSON(data []byte) (*Task, error) {
	var t Task
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, fmt.Errorf("models: unmarshal task: %w", err)
	}
	return &t, nil
}

// MessageToJSON serializes a Message to JSON bytes for jsonb storage.
func MessageToJSON(m *Message) ([]byte, error) {
	return json.Marshal(m)
}

// MessageFromJSON deserializes a Message from JSON bytes.
func MessageFromJSON(data []byte) (*Message, error) {
	var m Message
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("models: unmarshal message: %w", err)
	}
	return &m, nil
}

// PartsToJSON serializes Parts to JSON bytes for jsonb storage.
func PartsToJSON(parts []Part) ([]byte, error) {
	return json.Marshal(parts)
}

// PartsFromJSON deserializes Parts from JSON bytes.
func PartsFromJSON(data []byte) ([]Part, error) {
	var parts []Part
	if err := json.Unmarshal(data, &parts); err != nil {
		return nil, fmt.Errorf("models: unmarshal parts: %w", err)
	}
	return parts, nil
}

// AgentCardToJSON serializes an AgentCard to JSON bytes.
func AgentCardToJSON(c *AgentCard) ([]byte, error) {
	return json.Marshal(c)
}

// AgentCardFromJSON deserializes an AgentCard from JSON bytes.
func AgentCardFromJSON(data []byte) (*AgentCard, error) {
	var c AgentCard
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("models: unmarshal agent card: %w", err)
	}
	return &c, nil
}
