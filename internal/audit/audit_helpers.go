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
// empty string if no events have been recorded yet. The tip is the highest
// chain_seq (the order links were appended under writeMu), NOT the newest
// timestamp: callers may supply an out-of-band event Timestamp (e.g. the
// HITL sink mirrors engine events with their original timestamps) and two
// Records can take writeMu in a different order than their timestamps sort.
// Rows predating chain_seq fall back to timestamp order.
func (s *AuditService) latestHash(ctx context.Context) (string, error) {
	const q = `SELECT hash FROM audit_events ORDER BY COALESCE(chain_seq, -1) DESC, timestamp DESC, event_id DESC LIMIT 1`
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
	h.Write(canonicalDetails(ev.Details))
	h.Write([]byte{0})
	h.Write([]byte(ev.Outcome))
	return hex.EncodeToString(h.Sum(nil))
}

// detailsJSONForHash normalises raw stored details bytes to the canonical
// form computeHash expects (see canonicalDetails).
func detailsJSONForHash(details []byte) json.RawMessage {
	return canonicalDetails(json.RawMessage(details))
}

// canonicalDetails returns the deterministic JSON encoding used for hashing:
// null for empty input, otherwise the value re-marshalled through Go's
// encoding/json, which sorts object keys lexicographically and produces
// byte-identical output from equivalent JSON documents. The details column is
// JSONB, which re-serialises with its own key order (shortest key first, then
// alphabetical), so bytes stored in the database can differ from the bytes
// marshalled at write time. Both Record and the verifiers hash over this
// canonical form, so recomputation stays stable without rewriting the column.
func canonicalDetails(details json.RawMessage) json.RawMessage {
	if len(details) == 0 {
		return json.RawMessage("null")
	}
	var v any
	if err := json.Unmarshal(details, &v); err != nil {
		// Not valid JSON: hash the raw bytes as stored.
		return details
	}
	b, err := json.Marshal(v)
	if err != nil {
		return details
	}
	return json.RawMessage(b)
}

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
		return canonicalDetails(t), nil
	case []byte:
		return canonicalDetails(json.RawMessage(t)), nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	// Run through the same canonical form verification uses, so the hash
	// Record computes equals the hash GetEventChain recomputes from the
	// (possibly re-serialised by JSONB) stored bytes.
	return canonicalDetails(b), nil
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
