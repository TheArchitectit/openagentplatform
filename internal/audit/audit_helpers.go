package audit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// latestHash returns the hash of the most recently recorded event, or the
// empty string if no events have been recorded yet.
func (s *AuditService) latestHash(ctx context.Context) (string, error) {
	const q = `SELECT hash FROM audit_events ORDER BY timestamp DESC, event_id DESC LIMIT 1`
	var h string
	err := s.pool.QueryRow(ctx, q).Scan(&h)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
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
