// Package audit provides a tamper-evident, hash-chained audit log for
// OpenAgentPlatform. All platform actions (logins, API calls, agent actions,
// policy changes, etc.) are recorded with a SHA-256 hash that incorporates
// the hash of the preceding event, forming a Merkle-like chain that can be
// verified after the fact to detect tampering.
package audit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// EventType identifies the kind of platform event being recorded.
type EventType string

const (
	EventLogin        EventType = "login"
	EventLogout       EventType = "logout"
	EventAPICall      EventType = "api_call"
	EventAgentAction  EventType = "agent_action"
	EventCheckRun     EventType = "check_run"
	EventAlertChange  EventType = "alert_change"
	EventPolicyChange EventType = "policy_change"
	EventPatchDeploy  EventType = "patch_deploy"
	EventScriptRun    EventType = "script_run"
	EventUserManage   EventType = "user_manage"
	EventConfigChange EventType = "config_change"
)

// ActorType identifies the kind of principal that performed the action.
type ActorType string

const (
	ActorUser    ActorType = "user"
	ActorAgent   ActorType = "agent"
	ActorSystem  ActorType = "system"
	ActorAPIKey  ActorType = "api_key"
	ActorUnknown ActorType = "unknown"
)

// Outcome represents the result of the audited action.
type Outcome string

const (
	OutcomeSuccess Outcome = "success"
	OutcomeFailure Outcome = "failure"
	OutcomeDenied  Outcome = "denied"
	OutcomeError   Outcome = "error"
)

// Event is a single immutable audit log record.
type Event struct {
	EventID      string          `json:"event_id"`
	PrevHash     string          `json:"prev_hash"`
	Hash         string          `json:"hash"`
	Timestamp    time.Time       `json:"timestamp"`
	ActorType    ActorType       `json:"actor_type"`
	ActorID      string          `json:"actor_id"`
	Action       string          `json:"action"`
	ResourceType string          `json:"resource_type"`
	ResourceID   string          `json:"resource_id"`
	Details      json.RawMessage `json:"details,omitempty"`
	Outcome      Outcome         `json:"outcome"`
	IP           string          `json:"ip,omitempty"`
	UserAgent    string          `json:"user_agent,omitempty"`
	OrgID        string          `json:"org_id,omitempty"`
	SiteID       string          `json:"site_id,omitempty"`
}

// EventInput is the user-supplied subset of an Event; ID, hash, timestamp,
// and chain linkage are populated by Record.
type EventInput struct {
	ActorType    ActorType
	ActorID      string
	Action       string
	ResourceType string
	ResourceID   string
	Details      any
	Outcome      Outcome
	IP           string
	UserAgent    string
	OrgID        string
	SiteID       string
	Timestamp    time.Time // optional; defaults to time.Now().UTC()
}

// EventFilter is used by GetEvents to narrow results.
type EventFilter struct {
	ActorID      string
	Action       string
	ResourceType string
	ResourceID   string
	Since        time.Time
	Until        time.Time
	Limit        int
	Offset       int
}

// ChainLink is one link in the hash chain returned by GetEventChain.
type ChainLink struct {
	EventID   string    `json:"event_id"`
	PrevHash  string    `json:"prev_hash"`
	Hash      string    `json:"hash"`
	Timestamp time.Time `json:"timestamp"`
	Valid     bool      `json:"valid"`
}

// ChainVerification summarizes the integrity check of a chain. Intact means
// every link's stored hash recomputes from its own contents (no tampering).
// GapCount counts prev_hash discontinuities within the per-resource subset —
// expected, because the write-side chain is global and foreign-resource
// events interleave; gaps are not integrity failures.
type ChainVerification struct {
	ResourceID   string      `json:"resource_id"`
	Links        []ChainLink `json:"links"`
	Intact       bool        `json:"intact"`
	BrokenAt     string      `json:"broken_at,omitempty"`
	TotalChecked int         `json:"total_checked"`
	GapCount     int         `json:"gap_count"`
}

// AuditService records and queries audit events.
type AuditService struct {
	pool *pgxpool.Pool
	// writeMu serialises chain extension: latestHash + INSERT must be
	// atomic or two concurrent Records can fork the chain (both reading
	// the same prev hash, each becoming the other's sibling orphan).
	writeMu sync.Mutex
}

// New creates an AuditService backed by the given pgx pool.
func New(pool *pgxpool.Pool) *AuditService {
	return &AuditService{pool: pool}
}

// Record persists an audit event, computing its hash chain link and returning
// the fully populated Event. Chain extension is serialised in-process so
// concurrent Records cannot fork the hash chain.
func (s *AuditService) Record(ctx context.Context, in EventInput) (*Event, error) {
	if s == nil || s.pool == nil {
		return nil, fmt.Errorf("audit: service not initialised")
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if in.ActorType == "" {
		in.ActorType = ActorUnknown
	}
	if in.Outcome == "" {
		in.Outcome = OutcomeSuccess
	}
	if in.Timestamp.IsZero() {
		in.Timestamp = time.Now().UTC()
	}

	eventID := uuid.NewString()

	prevHash, err := s.latestHash(ctx)
	if err != nil {
		return nil, fmt.Errorf("audit: fetch prev hash: %w", err)
	}

	detailsJSON, err := marshalDetails(in.Details)
	if err != nil {
		return nil, fmt.Errorf("audit: marshal details: %w", err)
	}

	ev := &Event{
		EventID:      eventID,
		PrevHash:     prevHash,
		Timestamp:    in.Timestamp,
		ActorType:    in.ActorType,
		ActorID:      in.ActorID,
		Action:       in.Action,
		ResourceType: in.ResourceType,
		ResourceID:   in.ResourceID,
		Details:      detailsJSON,
		Outcome:      in.Outcome,
		IP:           in.IP,
		UserAgent:    in.UserAgent,
		OrgID:        in.OrgID,
		SiteID:       in.SiteID,
	}
	ev.Hash = computeHash(ev)

	const q = `
		INSERT INTO audit_events (
			event_id, prev_hash, hash, timestamp,
			actor_type, actor_id, action,
			resource_type, resource_id, details,
			outcome, ip, user_agent, org_id, site_id
		) VALUES (
			$1, $2, $3, $4,
			$5, $6, $7,
			$8, $9, $10,
			$11, $12, $13, $14, $15
		)
	`
	if _, err := s.pool.Exec(ctx, q,
		ev.EventID, ev.PrevHash, ev.Hash, ev.Timestamp,
		ev.ActorType, ev.ActorID, ev.Action,
		ev.ResourceType, ev.ResourceID, []byte(ev.Details),
		ev.Outcome, nullString(ev.IP), nullString(ev.UserAgent),
		nullString(ev.OrgID), nullString(ev.SiteID),
	); err != nil {
		return nil, fmt.Errorf("audit: insert event: %w", err)
	}
	return ev, nil
}

// GetEvent fetches a single event by ID. Returns ErrNotFound if missing.
func (s *AuditService) GetEvent(ctx context.Context, eventID string) (*Event, error) {
	if s == nil || s.pool == nil {
		return nil, fmt.Errorf("audit: service not initialised")
	}
	const q = `
		SELECT event_id, prev_hash, hash, timestamp,
		       actor_type, COALESCE(actor_id,''), action,
		       COALESCE(resource_type,''), COALESCE(resource_id,''),
		       details, outcome,
		       COALESCE(ip,''), COALESCE(user_agent,''),
		       COALESCE(org_id,''), COALESCE(site_id,'')
		FROM audit_events
		WHERE event_id = $1
		LIMIT 1
	`
	var ev Event
	var details []byte
	err := s.pool.QueryRow(ctx, q, eventID).Scan(
		&ev.EventID, &ev.PrevHash, &ev.Hash, &ev.Timestamp,
		&ev.ActorType, &ev.ActorID, &ev.Action,
		&ev.ResourceType, &ev.ResourceID,
		&details, &ev.Outcome,
		&ev.IP, &ev.UserAgent,
		&ev.OrgID, &ev.SiteID,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("audit: get event: %w", err)
	}
	if len(details) > 0 {
		ev.Details = json.RawMessage(details)
	}
	return &ev, nil
}
