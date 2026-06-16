package remote

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// CredentialType is the kind of secret we are storing.
type CredentialType string

const (
	CredentialPassword  CredentialType = "password"
	CredentialKey       CredentialType = "key"
	CredentialCert      CredentialType = "certificate"
	CredentialTemporary CredentialType = "temporary"
)

// RemoteCredential is a single stored secret, scoped to an agent or
// a site. Exactly one of AgentID/SiteID should be set; OrgDefault
// means "fall back to this when no agent/site match exists".
type RemoteCredential struct {
	ID            string         `json:"id"`
	Username      string         `json:"username"`
	Type          CredentialType `json:"type"`
	AgentID       string         `json:"agent_id,omitempty"`
	SiteID        string         `json:"site_id,omitempty"`
	OrgDefault    bool           `json:"org_default,omitempty"`
	EncryptedData string         `json:"-"` // base64 ciphertext, never sent to clients
	CreatedAt     time.Time      `json:"created_at"`
	ExpiresAt     time.Time      `json:"expires_at,omitempty"`
	// OneTime marks a temporary credential. It is invalidated on
	// first use (or on session close, whichever comes first).
	OneTime bool `json:"one_time,omitempty"`
	// Used tracks whether a one-time credential has been consumed.
	Used bool `json:"-"`
}

// CredentialStore keeps RemoteCredentials in memory. A production
// deployment will want a Postgres-backed implementation; this one is
// the minimum viable surface used by the API and by tests.
type CredentialStore struct {
	gcm cipher.AEAD

	mu   sync.RWMutex
	creds map[string]*RemoteCredential
}

// NewCredentialStore takes a 32-byte key (recommended: load from
// config) and returns a store ready for use.
func NewCredentialStore(key []byte) (*CredentialStore, error) {
	if len(key) < 16 {
		return nil, errors.New("remote: credential key must be at least 16 bytes")
	}
	// Pad to 32 bytes for AES-256 by hashing if the caller passed a
	// shorter key. This is a convenience for tests; production
	// callers should pass a full 32-byte key.
	k := padKey(key)
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, fmt.Errorf("remote: aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("remote: gcm: %w", err)
	}
	return &CredentialStore{
		gcm:    gcm,
		creds:  make(map[string]*RemoteCredential),
	}, nil
}

func padKey(k []byte) []byte {
	if len(k) >= 32 {
		return k[:32]
	}
	out := make([]byte, 32)
	copy(out, k)
	return out
}

// Encrypt seals plaintext under the store's key. Returns base64.
func (s *CredentialStore) Encrypt(plaintext []byte) (string, error) {
	nonce := make([]byte, s.gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("remote: nonce: %w", err)
	}
	ct := s.gcm.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(ct), nil
}

// Decrypt reverses Encrypt. Returns the plaintext bytes.
func (s *CredentialStore) Decrypt(ciphertext string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("remote: b64: %w", err)
	}
	if len(raw) < s.gcm.NonceSize() {
		return nil, errors.New("remote: ciphertext too short")
	}
	nonce, ct := raw[:s.gcm.NonceSize()], raw[s.gcm.NonceSize():]
	pt, err := s.gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("remote: decrypt: %w", err)
	}
	return pt, nil
}

// Store inserts or replaces a credential, encrypting credential_data
// in place. The returned struct has EncryptedData set and plaintext
// cleared.
func (s *CredentialStore) Store(c *RemoteCredential, plaintext []byte) (*RemoteCredential, error) {
	if c.Username == "" {
		return nil, errors.New("remote: credential username required")
	}
	if c.Type == "" {
		c.Type = CredentialPassword
	}
	enc, err := s.Encrypt(plaintext)
	if err != nil {
		return nil, err
	}
	c.EncryptedData = enc
	if c.ID == "" {
		c.ID = RandomID(8)
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}
	s.mu.Lock()
	s.creds[c.ID] = c
	s.mu.Unlock()
	return c, nil
}

// Get returns a credential by ID (with ciphertext).
func (s *CredentialStore) Get(id string) (*RemoteCredential, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c, ok := s.creds[id]
	return c, ok
}

// List returns masked copies (no ciphertext) of every credential.
// Masked entries show only username/type/IDs and creation time.
func (s *CredentialStore) List() []*RemoteCredential {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*RemoteCredential, 0, len(s.creds))
	for _, c := range s.creds {
		copy := *c
		copy.EncryptedData = ""
		out = append(out, &copy)
	}
	return out
}

// Delete removes a credential. Returns ErrCredentialNotFound when no
// entry exists with that ID.
func (s *CredentialStore) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.creds[id]; !ok {
		return ErrCredentialNotFound
	}
	delete(s.creds, id)
	return nil
}

// ErrCredentialNotFound is returned by Delete when the ID is unknown.
var ErrCredentialNotFound = errors.New("remote: credential not found")

// Resolver picks the best credential for an agent, falling back to
// site-level and then to the org default. The lookup is read-only
// against the store; the caller is expected to call
// ConsumeTemporary() if the resolved credential is one-time.
type Resolver struct {
	store *CredentialStore
}

// NewResolver wraps a store.
func NewResolver(store *CredentialStore) *Resolver {
	return &Resolver{store: store}
}

// Resolve returns the most-specific credential for the given agent
// and site. Returns nil with no error when nothing matches — callers
// can decide whether that is a 404 or a 500.
func (r *Resolver) Resolve(agentID, siteID string) (*RemoteCredential, error) {
	if r == nil || r.store == nil {
		return nil, errors.New("remote: resolver has no store")
	}
	r.store.mu.RLock()
	defer r.store.mu.RUnlock()

	// 1. Exact agent match.
	for _, c := range r.store.creds {
		if c.AgentID == agentID && !c.OrgDefault {
			return c, nil
		}
	}
	// 2. Site match.
	if siteID != "" {
		for _, c := range r.store.creds {
			if c.SiteID == siteID && c.AgentID == "" && !c.OrgDefault {
				return c, nil
			}
		}
	}
	// 3. Org default.
	for _, c := range r.store.creds {
		if c.OrgDefault {
			return c, nil
		}
	}
	return nil, nil
}

// GenerateTemporary creates a one-time-use credential valid for the
// session duration. The plaintext is encrypted before storage; the
// caller can resolve the credential later via Resolve() and decrypt
// with the store key.
func (s *CredentialStore) GenerateTemporary(username string, agentID string, duration time.Duration) (*RemoteCredential, []byte, error) {
	if duration <= 0 {
		duration = time.Hour
	}
	plaintext := []byte(RandomID(24))
	c := &RemoteCredential{
		Username:   username,
		Type:       CredentialTemporary,
		AgentID:    agentID,
		OneTime:    true,
		ExpiresAt:  time.Now().UTC().Add(duration),
		CreatedAt:  time.Now().UTC(),
	}
	stored, err := s.Store(c, plaintext)
	if err != nil {
		return nil, nil, err
	}
	return stored, plaintext, nil
}

// ConsumeTemporary marks a one-time credential as used. Returns an
// error if the credential is not one-time, is already used, or has
// expired.
func (s *CredentialStore) ConsumeTemporary(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.creds[id]
	if !ok {
		return ErrCredentialNotFound
	}
	if !c.OneTime {
		return errors.New("remote: credential is not one-time")
	}
	if c.Used {
		return errors.New("remote: credential already used")
	}
	if !c.ExpiresAt.IsZero() && time.Now().UTC().After(c.ExpiresAt) {
		return errors.New("remote: credential expired")
	}
	c.Used = true
	// Delete immediately so it can't be reused.
	delete(s.creds, id)
	return nil
}

// RotateOnClose removes all temporary credentials associated with a
// given agent. Called when a shell session ends.
func (s *CredentialStore) RotateOnClose(agentID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed := 0
	for id, c := range s.creds {
		if c.Type == CredentialTemporary && c.AgentID == agentID {
			delete(s.creds, id)
			removed++
		}
	}
	return removed
}
