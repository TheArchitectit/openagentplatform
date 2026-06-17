// Package registry - store.go implements the PostgreSQL persistence layer
// for A2A AgentCard registration. It stores agent card metadata as JSONB
// columns keyed by the agent's URL.
package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ============================================================
// SQL schema (DDL)
// ============================================================

// AgentCardSchema returns the DDL statement for the a2a_agent_cards table.
// Callers should execute this during database initialization.
//
// Columns:
//   url              TEXT PK     — unique agent URL / endpoint
//   name             TEXT        — human-readable agent name
//   description      TEXT        — agent description
//   version          TEXT        — agent version string
//   provider         JSONB       — provider metadata (organization, url, etc.)
//   skills           JSONB       — array of skill objects (id, name, tags, ...)
//   streaming        BOOLEAN     — supports streaming responses
//   push_notifications BOOLEAN   — supports push notification callbacks
//   auth_schemes     JSONB       — array of authentication scheme descriptors
//   last_heartbeat   TIMESTAMPTZ — last time the agent sent a heartbeat
//   created_at       TIMESTAMPTZ — when the card was first registered
//   updated_at       TIMESTAMPTZ — when the card was last updated
const AgentCardSchema = `
CREATE TABLE IF NOT EXISTS a2a_agent_cards (
	url                TEXT         PRIMARY KEY,
	name               TEXT         NOT NULL DEFAULT '',
	description        TEXT         NOT NULL DEFAULT '',
	version            TEXT         NOT NULL DEFAULT '',
	provider           JSONB        NOT NULL DEFAULT '{}'::jsonb,
	skills             JSONB        NOT NULL DEFAULT '[]'::jsonb,
	streaming          BOOLEAN      NOT NULL DEFAULT FALSE,
	push_notifications BOOLEAN      NOT NULL DEFAULT FALSE,
	auth_schemes       JSONB        NOT NULL DEFAULT '[]'::jsonb,
	last_heartbeat     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
	created_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
	updated_at         TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_a2a_agent_cards_name ON a2a_agent_cards (name);
`

// ============================================================
// Store interface
// ============================================================

// CardStore is the persistence interface for AgentCard records.
// It abstracts over the database so the in-memory registry can be
// refreshed from any backing store.
type CardStore interface {
	// UpsertCard inserts or updates an agent card. The URL must be set.
	UpsertCard(ctx context.Context, card *AgentCardRow) error

	// DeleteCard removes an agent card by URL. Returns ErrCardNotFound
	// if the URL does not exist.
	DeleteCard(ctx context.Context, url string) error

	// GetCard fetches a single agent card row by URL.
	GetCard(ctx context.Context, url string) (*AgentCardRow, error)

	// ListCards returns all agent card rows, optionally filtered by name.
	ListCards(ctx context.Context) ([]AgentCardRow, error)
}

// ============================================================
// Row model
// ============================================================

// AgentCardRow is the database-level representation of an agent card.
// It mirrors the table columns and provides a flat structure suitable
// for JSONB serialization.
type AgentCardRow struct {
	URL              string         `json:"url"`
	Name             string         `json:"name"`
	Description      string         `json:"description"`
	Version          string         `json:"version"`
	Provider         map[string]any `json:"provider"`
	Skills           []byte         `json:"-"` // raw JSON array of skill objects
	Streaming        bool           `json:"streaming"`
	PushNotifications bool          `json:"push_notifications"`
	AuthSchemes      []byte         `json:"-"` // raw JSON array of auth scheme objects
	LastHeartbeat    string         `json:"last_heartbeat"` // ISO 8601 timestamp
	CreatedAt        string         `json:"created_at"`
	UpdatedAt        string         `json:"updated_at"`
}

// ============================================================
// Errors
// ============================================================

// ErrCardNotFound is returned when a card URL does not exist.
var ErrCardNotFound = errors.New("a2a: agent card not found")

// ErrCardURLRequired is returned when an operation requires a non-empty URL.
var ErrCardURLRequired = errors.New("a2a: agent card URL is required")

// ============================================================
// PostgreSQL implementation
// ============================================================

// pgCardStore is the default PostgreSQL-backed implementation of CardStore.
type pgCardStore struct {
	pool *pgxpool.Pool
}

// NewPGCardStore constructs a CardStore backed by a pgx connection pool.
func NewPGCardStore(pool *pgxpool.Pool) CardStore {
	return &pgCardStore{pool: pool}
}

// UpsertCard inserts or updates an agent card row. The URL must be set.
// On conflict (duplicate URL), all fields are updated and the
// updated_at / last_heartbeat timestamps are refreshed.
func (s *pgCardStore) UpsertCard(ctx context.Context, card *AgentCardRow) error {
	if s.pool == nil {
		return errors.New("a2a: nil pool")
	}
	if card.URL == "" {
		return ErrCardURLRequired
	}

	providerJSON, err := jsonOrNullMap(card.Provider)
	if err != nil {
		return fmt.Errorf("a2a: marshal provider: %w", err)
	}
	skillsJSON, err := jsonOrNullBytes(card.Skills)
	if err != nil {
		return fmt.Errorf("a2a: marshal skills: %w", err)
	}
	authJSON, err := jsonOrNullBytes(card.AuthSchemes)
	if err != nil {
		return fmt.Errorf("a2a: marshal auth schemes: %w", err)
	}

	const q = `
		INSERT INTO a2a_agent_cards (
			url, name, description, version, provider, skills,
			streaming, push_notifications, auth_schemes,
			last_heartbeat, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9,
			NOW(), NOW(), NOW()
		)
		ON CONFLICT (url) DO UPDATE SET
			name               = EXCLUDED.name,
			description        = EXCLUDED.description,
			version            = EXCLUDED.version,
			provider           = EXCLUDED.provider,
			skills             = EXCLUDED.skills,
			streaming          = EXCLUDED.streaming,
			push_notifications = EXCLUDED.push_notifications,
			auth_schemes       = EXCLUDED.auth_schemes,
			last_heartbeat     = NOW(),
			updated_at         = NOW()
	`
	_, err = s.pool.Exec(ctx, q,
		card.URL, card.Name, card.Description, card.Version,
		providerJSON, skillsJSON,
		card.Streaming, card.PushNotifications, authJSON,
	)
	if err != nil {
		return fmt.Errorf("a2a: upsert agent card: %w", err)
	}
	return nil
}

// DeleteCard removes an agent card by URL.
func (s *pgCardStore) DeleteCard(ctx context.Context, url string) error {
	if s.pool == nil {
		return errors.New("a2a: nil pool")
	}
	if url == "" {
		return ErrCardURLRequired
	}
	const q = `DELETE FROM a2a_agent_cards WHERE url = $1`
	tag, err := s.pool.Exec(ctx, q, url)
	if err != nil {
		return fmt.Errorf("a2a: delete agent card: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrCardNotFound
	}
	return nil
}

// GetCard fetches a single agent card by URL.
func (s *pgCardStore) GetCard(ctx context.Context, url string) (*AgentCardRow, error) {
	if s.pool == nil {
		return nil, errors.New("a2a: nil pool")
	}
	if url == "" {
		return nil, ErrCardURLRequired
	}
	const q = `
		SELECT url, COALESCE(name,''), COALESCE(description,''),
		       COALESCE(version,''), provider, skills,
		       streaming, push_notifications, auth_schemes,
		       last_heartbeat, created_at, updated_at
		FROM a2a_agent_cards
		WHERE url = $1
		LIMIT 1
	`
	var row AgentCardRow
	var providerRaw []byte
	err := s.pool.QueryRow(ctx, q, url).Scan(
		&row.URL, &row.Name, &row.Description, &row.Version,
		&providerRaw, &row.Skills,
		&row.Streaming, &row.PushNotifications, &row.AuthSchemes,
		&row.LastHeartbeat, &row.CreatedAt, &row.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCardNotFound
		}
		return nil, fmt.Errorf("a2a: get agent card: %w", err)
	}
	if len(providerRaw) > 0 {
		_ = json.Unmarshal(providerRaw, &row.Provider)
	}
	return &row, nil
}

// ListCards returns all agent card rows.
func (s *pgCardStore) ListCards(ctx context.Context) ([]AgentCardRow, error) {
	if s.pool == nil {
		return nil, errors.New("a2a: nil pool")
	}
	const q = `
		SELECT url, COALESCE(name,''), COALESCE(description,''),
		       COALESCE(version,''), provider, skills,
		       streaming, push_notifications, auth_schemes,
		       last_heartbeat, created_at, updated_at
		FROM a2a_agent_cards
		ORDER BY name ASC
	`
	rows, err := s.pool.Query(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("a2a: list agent cards: %w", err)
	}
	defer rows.Close()

	out := make([]AgentCardRow, 0, 8)
	for rows.Next() {
		var row AgentCardRow
		var providerRaw []byte
		if err := rows.Scan(
			&row.URL, &row.Name, &row.Description, &row.Version,
			&providerRaw, &row.Skills,
			&row.Streaming, &row.PushNotifications, &row.AuthSchemes,
			&row.LastHeartbeat, &row.CreatedAt, &row.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("a2a: scan agent card: %w", err)
		}
		if len(providerRaw) > 0 {
			_ = json.Unmarshal(providerRaw, &row.Provider)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("a2a: rows err: %w", err)
	}
	return out, nil
}

// ============================================================
// Helpers
// ============================================================

// jsonOrNullMap marshals a map to JSON, returning an empty object if nil.
func jsonOrNullMap(m map[string]any) ([]byte, error) {
	if len(m) == 0 {
		return []byte(`{}`), nil
	}
	return json.Marshal(m)
}

// jsonOrNullBytes returns the bytes as-is if non-nil, or an empty JSON array.
func jsonOrNullBytes(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return []byte(`[]`), nil
	}
	return data, nil
}
