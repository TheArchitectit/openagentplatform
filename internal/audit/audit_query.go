package audit

import (
	"context"
	"encoding/json"
	"fmt"
)

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
