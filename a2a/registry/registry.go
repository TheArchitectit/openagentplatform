// Package registry - registry.go implements an in-memory AgentCard
// registry backed by PostgreSQL. It supports concurrent access via
// sync.RWMutex, heartbeat-based staleness detection, and periodic
// pruning of expired cards.
package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/openagentplatform/openagentplatform/a2a/models"
)

// ============================================================
// Configuration
// ============================================================

// Config holds registry tuning parameters.
type Config struct {
	// HeartbeatTTL is the duration after which an agent card is considered
	// stale if no heartbeat has been received. Default: 90s.
	HeartbeatTTL time.Duration

	// PruneInterval is how often the background pruning loop sweeps for
	// stale cards. Default: 30s.
	PruneInterval time.Duration

	// EnableAutoPrune controls whether the background pruning goroutine
	// runs. Set to false for tests. Default: true.
	EnableAutoPrune bool
}

// DefaultConfig returns a Config with sensible production defaults.
func DefaultConfig() Config {
	return Config{
		HeartbeatTTL:    90 * time.Second,
		PruneInterval:   30 * time.Second,
		EnableAutoPrune: true,
	}
}

// ============================================================
// Registry errors
// ============================================================

var (
	// ErrCardNotFoundInRegistry is returned when a card URL is not in the
	// in-memory registry.
	ErrCardNotFoundInRegistry = errors.New("a2a: card not found in registry")

	// ErrInvalidCard is returned when a card fails validation.
	ErrInvalidCard = errors.New("a2a: invalid agent card")
)

// ============================================================
// Registry
// ============================================================

// entry is an internal record that pairs an agent card with its
// last-heartbeat timestamp.
type entry struct {
	card          models.AgentCard
	lastHeartbeat time.Time
}

// Registry is an in-memory store of agent cards with PostgreSQL
// persistence and heartbeat-based staleness detection.
type Registry struct {
	store  CardStore
	config Config

	mu      sync.RWMutex
	cards   map[string]*entry

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewRegistry constructs a Registry backed by the given CardStore
// (typically NewPGCardStore(pool)). It immediately calls
// RefreshFromDB to populate the in-memory cache. If auto-prune is
// enabled in the config, a background goroutine is started.
func NewRegistry(ctx context.Context, store CardStore, cfg Config) (*Registry, error) {
	if store == nil {
		return nil, errors.New("a2a: nil card store")
	}
	if cfg.HeartbeatTTL <= 0 {
		cfg.HeartbeatTTL = 90 * time.Second
	}
	if cfg.PruneInterval <= 0 {
		cfg.PruneInterval = 30 * time.Second
	}

	r := &Registry{
		store:  store,
		config: cfg,
		cards:  make(map[string]*entry),
		stopCh: make(chan struct{}),
	}

	// Initial load from DB
	if err := r.RefreshFromDB(ctx); err != nil {
		return nil, fmt.Errorf("a2a: initial registry load: %w", err)
	}

	if cfg.EnableAutoPrune {
		r.wg.Add(1)
		go r.pruneLoop()
	}

	return r, nil
}

// ============================================================
// Register / Unregister
// ============================================================

// Register adds or updates an agent card in the registry and persists
// it to the backing store. The card's URL is used as the unique key.
// The last-heartbeat is set to time.Now(). Returns an error if the
// card fails validation or the database write fails.
func (r *Registry) Register(ctx context.Context, card *models.AgentCard) error {
	if card == nil {
		return fmt.Errorf("%w: nil card", ErrInvalidCard)
	}
	if err := card.Validate(); err != nil {
		return fmt.Errorf("%w: %s", ErrInvalidCard, err)
	}

	// Persist to DB first, then update in-memory cache.
	row := cardToRow(card)
	if err := r.store.UpsertCard(ctx, row); err != nil {
		return fmt.Errorf("a2a: register: persist: %w", err)
	}

	r.mu.Lock()
	r.cards[card.URL] = &entry{
		card:          *card,
		lastHeartbeat: time.Now().UTC(),
	}
	r.mu.Unlock()

	return nil
}

// Unregister removes an agent card from the registry and the database.
// Returns ErrCardNotFoundInRegistry if the card is not in the in-memory
// cache. The database delete is best-effort; if it returns
// ErrCardNotFound, the error is ignored (the card may have already
// been removed from the DB).
func (r *Registry) Unregister(ctx context.Context, url string) error {
	if url == "" {
		return errors.New("a2a: url required for unregister")
	}

	// Remove from in-memory cache first.
	r.mu.Lock()
	_, ok := r.cards[url]
	if ok {
		delete(r.cards, url)
	}
	r.mu.Unlock()

	if !ok {
		return ErrCardNotFoundInRegistry
	}

	// Best-effort DB delete.
	if err := r.store.DeleteCard(ctx, url); err != nil && !errors.Is(err, ErrCardNotFound) {
		return fmt.Errorf("a2a: unregister: persist: %w", err)
	}
	return nil
}

// ============================================================
// GetCard / ListCards
// ============================================================

// GetCard retrieves an agent card by URL. Stale cards (past the
// heartbeat TTL) are still returned but marked as stale via the
// returned entry's lastHeartbeat timestamp. To filter out stale
// cards, use ListCards with the skip-stale option.
func (r *Registry) GetCard(url string) (*models.AgentCard, error) {
	if url == "" {
		return nil, errors.New("a2a: url required")
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	e, ok := r.cards[url]
	if !ok {
		return nil, ErrCardNotFoundInRegistry
	}
	cp := e.card
	return &cp, nil
}

// ListCards returns all in-memory agent cards. If skipStale is true,
// cards whose last heartbeat is older than the configured TTL are
// excluded.
func (r *Registry) ListCards(skipStale bool) []models.AgentCard {
	r.mu.RLock()
	defer r.mu.RUnlock()

	now := time.Now().UTC()
	cutoff := now.Add(-r.config.HeartbeatTTL)

	out := make([]models.AgentCard, 0, len(r.cards))
	for _, e := range r.cards {
		if skipStale && e.lastHeartbeat.Before(cutoff) {
			continue
		}
		out = append(out, e.card)
	}
	return out
}

// ============================================================
// Heartbeat
// ============================================================

// Heartbeat updates the last-heartbeat timestamp for an agent card.
// Returns ErrCardNotFoundInRegistry if the card is not registered.
// The timestamp is also written to the database.
func (r *Registry) Heartbeat(ctx context.Context, url string) error {
	if url == "" {
		return errors.New("a2a: url required for heartbeat")
	}

	r.mu.Lock()
	e, ok := r.cards[url]
	if !ok {
		r.mu.Unlock()
		return ErrCardNotFoundInRegistry
	}
	e.lastHeartbeat = time.Now().UTC()
	r.mu.Unlock()

	// Update DB timestamp via UpsertCard with current state.
	r.mu.RLock()
	row := cardToRow(&e.card)
	r.mu.RUnlock()

	if err := r.store.UpsertCard(ctx, row); err != nil {
		return fmt.Errorf("a2a: heartbeat: persist: %w", err)
	}
	return nil
}

// PruneStale removes all cards whose last heartbeat is older than the
// configured TTL. Returns the URLs of the cards that were removed.
func (r *Registry) PruneStale(ctx context.Context) []string {
	now := time.Now().UTC()
	cutoff := now.Add(-r.config.HeartbeatTTL)

	r.mu.Lock()
	var staleURLs []string
	for url, e := range r.cards {
		if e.lastHeartbeat.Before(cutoff) {
			staleURLs = append(staleURLs, url)
		}
	}
	for _, url := range staleURLs {
		delete(r.cards, url)
	}
	r.mu.Unlock()

	// Best-effort DB cleanup
	for _, url := range staleURLs {
		_ = r.store.DeleteCard(ctx, url)
	}
	return staleURLs
}

// ============================================================
// RefreshFromDB
// ============================================================

// RefreshFromDB reloads all agent cards from the database into the
// in-memory cache. This replaces the entire in-memory state. It is
// safe to call at any time (e.g., periodically or after a known
// external change).
func (r *Registry) RefreshFromDB(ctx context.Context) error {
	rows, err := r.store.ListCards(ctx)
	if err != nil {
		return fmt.Errorf("a2a: refresh: list: %w", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.cards = make(map[string]*entry, len(rows))
	for _, row := range rows {
		card, err := rowToCard(&row)
		if err != nil {
			// Skip corrupt rows but continue loading.
			continue
		}
		r.cards[row.URL] = &entry{
			card:          *card,
			lastHeartbeat: parseTimestamp(row.LastHeartbeat),
		}
	}
	return nil
}

// ============================================================
// Lifecycle
// ============================================================

// Close stops the background pruning goroutine and waits for it to exit.
func (r *Registry) Close() {
	close(r.stopCh)
	r.wg.Wait()
}

// Size returns the number of cards currently in the in-memory registry.
func (r *Registry) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.cards)
}

// ============================================================
// Background pruning
// ============================================================

func (r *Registry) pruneLoop() {
	defer r.wg.Done()

	ticker := time.NewTicker(r.config.PruneInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopCh:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			r.PruneStale(ctx)
			cancel()
		}
	}
}

// ============================================================
// Row <-> Card conversion helpers
// ============================================================

// cardToRow converts a domain AgentCard to a database row.
func cardToRow(card *models.AgentCard) *AgentCardRow {
	providerMap := map[string]any{
		"organization": card.ProviderName,
	}

	skillsJSON, _ := json.Marshal(card.Skills)
	authJSON, _ := json.Marshal(card.AuthSchemes)

	return &AgentCardRow{
		URL:               card.URL,
		Name:              card.Name,
		Description:       card.Description,
		Version:           card.Version,
		Provider:          providerMap,
		Skills:            skillsJSON,
		Streaming:         card.Streaming,
		PushNotifications: card.PushNotifications,
		AuthSchemes:       authJSON,
	}
}

// rowToCard converts a database row back to a domain AgentCard.
func rowToCard(row *AgentCardRow) (*models.AgentCard, error) {
	card := &models.AgentCard{
		ID:                row.URL,
		Name:              row.Name,
		Description:       row.Description,
		Version:           row.Version,
		URL:               row.URL,
		ProviderName:      stringFromMap(row.Provider, "organization"),
		Streaming:         row.Streaming,
		PushNotifications: row.PushNotifications,
	}

	// Deserialize skills
	if len(row.Skills) > 0 {
		if err := json.Unmarshal(row.Skills, &card.Skills); err != nil {
			return nil, fmt.Errorf("unmarshal skills: %w", err)
		}
	}
	if card.Skills == nil {
		card.Skills = []models.Skill{}
	}

	// Collect tags from all skills
	tagSet := make(map[string]struct{})
	for _, s := range card.Skills {
		for _, t := range s.Tags {
			tagSet[t] = struct{}{}
		}
	}
	for t := range tagSet {
		card.Tags = append(card.Tags, t)
	}

	// Deserialize auth schemes
	if len(row.AuthSchemes) > 0 && string(row.AuthSchemes) != "null" {
		var auths []models.AuthScheme
		if err := json.Unmarshal(row.AuthSchemes, &auths); err == nil && len(auths) > 0 {
			card.AuthSchemes = auths
		}
	}

	return card, nil
}

// parseTimestamp parses a PostgreSQL timestamp string. It accepts
// several common formats. Falls back to time.Now() on parse error.
func parseTimestamp(s string) time.Time {
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999-07",
		"2006-01-02 15:04:05.999999Z",
		"2006-01-02 15:04:05-07",
		"2006-01-02 15:04:05Z",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t.UTC()
		}
	}
	return time.Now().UTC()
}

// containsString checks if a slice contains a string.
func containsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// stringFromMap retrieves a string value from a map[string]any.
func stringFromMap(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
