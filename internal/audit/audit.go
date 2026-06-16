// Package audit provides a tamper-evident, hash-chained audit log for
// OpenAgentPlatform. All platform actions (logins, API calls, agent actions,
// policy changes, etc.) are recorded with a SHA-256 hash that incorporates
// the hash of the preceding event, forming a Merkle-like chain that can be
// verified after the fact to detect tampering.
package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
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

// ChainVerification summarizes the integrity check of a chain.
type ChainVerification struct {
	ResourceID   string      `json:"resource_id"`
	Links        []ChainLink `json:"links"`
	Intact       bool        `json:"intact"`
	BrokenAt     string      `json:"broken_at,omitempty"`
	TotalChecked int         `json:"total_checked"`
}

// AuditService records and queries audit events.
type AuditService struct {
	pool *pgxpool.Pool
}

// New creates an AuditService backed by the given pgx pool.
func New(pool *pgxpool.Pool) *AuditService {
	return &AuditService{pool: pool}
}

// Record persists an audit event, computing its hash chain link and returning
// the fully populated Event.
func (s *AuditService) Record(ctx context.Context, in EventInput) (*Event, error) {
	if s == nil || s.pool == nil {
		return nil, fmt.Errorf("audit: service not initialised")
	}
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

// GetEvents returns events matching the filter, plus the total matching row
// count.
func (s *AuditService) GetEvents(ctx context.Context, f EventFilter) ([]Event, int, error) {
	if s == nil || s.pool == nil {
		return nil, 0, fmt.Errorf("audit: service not initialised")
	}
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	if f.Offset < 0 {
		f.Offset = 0
	}

	args := make([]any, 0, 6)
	conds := make([]string, 0, 6)
	add := func(clause string, val any) {
		args = append(args, val)
		conds = append(conds, fmt.Sprintf(clause, len(args)))
	}
	if f.ActorID != "" {
		add("actor_id = $%d", f.ActorID)
	}
	if f.Action != "" {
		add("action = $%d", f.Action)
	}
	if f.ResourceType != "" {
		add("resource_type = $%d", f.ResourceType)
	}
	if f.ResourceID != "" {
		add("resource_id = $%d", f.ResourceID)
	}
	if !f.Since.IsZero() {
		add("timestamp >= $%d", f.Since)
	}
	if !f.Until.IsZero() {
		add("timestamp <= $%d", f.Until)
	}
	whereSQL := ""
	if len(conds) > 0 {
		whereSQL = "WHERE " + joinAnd(conds)
	}

	var total int
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM audit_events "+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("audit: count events: %w", err)
	}

	args = append(args, f.Limit, f.Offset)
	q := fmt.Sprintf(`
		SELECT event_id, prev_hash, hash, timestamp,
		       actor_type, COALESCE(actor_id,''), action,
		       COALESCE(resource_type,''), COALESCE(resource_id,''),
		       details, outcome,
		       COALESCE(ip,''), COALESCE(user_agent,''),
		       COALESCE(org_id,''), COALESCE(site_id,'')
		FROM audit_events
		%s
		ORDER BY timestamp DESC
		LIMIT $%d OFFSET $%d
	`, whereSQL, len(args)-1, len(args))

	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("audit: list events: %w", err)
	}
	defer rows.Close()

	out := make([]Event, 0, f.Limit)
	for rows.Next() {
		var ev Event
		var details []byte
		if err := rows.Scan(
			&ev.EventID, &ev.PrevHash, &ev.Hash, &ev.Timestamp,
			&ev.ActorType, &ev.ActorID, &ev.Action,
			&ev.ResourceType, &ev.ResourceID,
			&details, &ev.Outcome,
			&ev.IP, &ev.UserAgent,
			&ev.OrgID, &ev.SiteID,
		); err != nil {
			return nil, 0, fmt.Errorf("audit: scan event: %w", err)
		}
		if len(details) > 0 {
			ev.Details = json.RawMessage(details)
		}
		out = append(out, ev)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("audit: rows err: %w", err)
	}
	return out, total, nil
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
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("audit: get event: %w", err)
	}
	if len(details) > 0 {
		ev.Details = json.RawMessage(details)
	}
	return &ev, nil
}

// GetEventChain returns the hash chain for a given resource ID and verifies
// each link. The chain is ordered from oldest to newest.
func (s *AuditService) GetEventChain(ctx context.Context, resourceID string) (*ChainVerification, error) {
	if s == nil || s.pool == nil {
		return nil, fmt.Errorf("audit: service not initialised")
	}
	const q = `
		SELECT event_id, prev_hash, hash, timestamp
		FROM audit_events
		WHERE resource_id = $1
		ORDER BY timestamp ASC, event_id ASC
	`
	rows, err := s.pool.Query(ctx, q, resourceID)
	if err != nil {
		return nil, fmt.Errorf("audit: list chain: %w", err)
	}
	defer rows.Close()

	ver := &ChainVerification{ResourceID: resourceID, Links: []ChainLink{}, Intact: true}
	var prev string
	for rows.Next() {
		var link ChainLink
		if err := rows.Scan(&link.EventID, &link.PrevHash, &link.Hash, &link.Timestamp); err != nil {
			return nil, fmt.Errorf("audit: scan chain link: %w", err)
		}
		// The first link should be the genesis (empty prev hash); subsequent
		// links should reference the prior event's hash.
		if link.PrevHash != prev {
			link.Valid = false
			ver.Intact = false
			if ver.BrokenAt == "" {
				ver.BrokenAt = link.EventID
			}
		} else {
			link.Valid = true
		}
		ver.Links = append(ver.Links, link)
		prev = link.Hash
		ver.TotalChecked++
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("audit: chain rows err: %w", err)
	}
	return ver, nil
}

// latestHash returns the hash of the most recently recorded event, or the
// empty string if no events have been recorded yet.
func (s *AuditService) latestHash(ctx context.Context) (string, error) {
	const q = `SELECT hash FROM audit_events ORDER BY timestamp DESC, event_id DESC LIMIT 1`
	var h string
	err := s.pool.QueryRow(ctx, q).Scan(&h)
	if err != nil {
		if err == pgx.ErrNoRows {
			return "", nil
		}
		return "", err
	}
	return h, nil
}

// computeHash returns the hex-encoded SHA-256 of the canonical event
// representation. Any change to the hash function or field order is a
// breaking change and requires a migration of the stored chain.
func computeHash(ev *Event) string {
	h := sha256.New()
	h.Write([]byte(ev.EventID))
	h.Write([]byte{0})
	h.Write([]byte(ev.PrevHash))
	h.Write([]byte{0})
	h.Write([]byte(ev.Timestamp.UTC().Format(time.RFC3339Nano)))
	h.Write([]byte{0})
	h.Write([]byte(ev.ActorType))
	h.Write([]byte{0})
	h.Write([]byte(ev.ActorID))
	h.Write([]byte{0})
	h.Write([]byte(ev.Action))
	h.Write([]byte{0})
	h.Write([]byte(ev.ResourceType))
	h.Write([]byte{0})
	h.Write([]byte(ev.ResourceID))
	h.Write([]byte{0})
	if len(ev.Details) == 0 {
		h.Write([]byte("null"))
	} else {
		h.Write(ev.Details)
	}
	h.Write([]byte{0})
	h.Write([]byte(ev.Outcome))
	return hex.EncodeToString(h.Sum(nil))
}

// VerifyHash recomputes the hash for an event and compares it to the stored
// value. Useful for clients that want to spot-check a single event.
func VerifyHash(ev *Event) bool {
	if ev == nil {
		return false
	}
	return computeHash(ev) == ev.Hash
}

// marshalDetails serialises the details field. nil becomes the JSON null
// literal so the hash is stable across equivalent inputs.
func marshalDetails(v any) (json.RawMessage, error) {
	if v == nil {
		return json.RawMessage("null"), nil
	}
	switch t := v.(type) {
	case json.RawMessage:
		if len(t) == 0 {
			return json.RawMessage("null"), nil
		}
		return t, nil
	case []byte:
		if len(t) == 0 {
			return json.RawMessage("null"), nil
		}
		return json.RawMessage(t), nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(b), nil
}

func nullString(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func joinAnd(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " AND "
		}
		out += p
	}
	return out
}

// ErrNotFound is returned when an event id is not present in the log.
var ErrNotFound = fmt.Errorf("audit: event not found")
