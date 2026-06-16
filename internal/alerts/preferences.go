// Package alerts - preferences.go defines user-level and global alert
// preferences and the evaluator that decides whether a given alert should
// be delivered to a given user.
package alerts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// DayOfWeek identifies a day in the quiet-hours schedule. Values match
// time.Weekday for convenience (Sunday = 0).
type DayOfWeek int

const (
	Sunday    DayOfWeek = 0
	Monday    DayOfWeek = 1
	Tuesday   DayOfWeek = 2
	Wednesday DayOfWeek = 3
	Thursday  DayOfWeek = 4
	Friday    DayOfWeek = 5
	Saturday  DayOfWeek = 6
)

// QuietHours describes a daily window during which notifications are
// suppressed. Start and End are "HH:MM" 24-hour times in the user's
// configured Timezone. Days lists the days of the week (0=Sunday) the
// window applies to. If End <= Start the window is treated as
// overnight (wraps past midnight).
type QuietHours struct {
	StartTime string    `json:"start_time"`
	EndTime   string    `json:"end_time"`
	Timezone  string    `json:"timezone"`
	Days      []DayOfWeek `json:"days"`
}

// UserAlertPreferences is the per-user configuration that controls
// whether a specific user should receive an alert.
type UserAlertPreferences struct {
	UserID              string     `json:"user_id"`
	OrgID               string     `json:"org_id"`
	QuietHours          QuietHours `json:"quiet_hours"`
	SeverityThreshold   string     `json:"severity_threshold"`   // info, warning, critical, emergency
	ChannelPreferences  map[string]bool `json:"channel_preferences"` // channel type -> enabled
	MuteAll             bool       `json:"mute_all"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

// GlobalAlertPreferences is the org-wide default configuration. It is
// read by the engine when a user has no explicit preferences or when
// admin policy overrides user choice.
type GlobalAlertPreferences struct {
	OrgID                 string `json:"org_id"`
	DefaultQuietHours     QuietHours `json:"default_quiet_hours"`
	RetentionDays         int    `json:"retention_days"`
	MaxAlertsPerAgent     int    `json:"max_alerts_per_agent"`
	AutoResolveSeconds    int    `json:"auto_resolve_seconds"`
	UpdatedAt             time.Time `json:"updated_at"`
}

// PreferenceStore is the persistence seam for alert preferences. The
// engine and API use this interface; pgAlertStore is the default
// implementation.
type PreferenceStore interface {
	GetUserPreferences(ctx context.Context, userID, orgID string) (*UserAlertPreferences, error)
	UpsertUserPreferences(ctx context.Context, p *UserAlertPreferences) error
	GetGlobalPreferences(ctx context.Context, orgID string) (*GlobalAlertPreferences, error)
	UpsertGlobalPreferences(ctx context.Context, p *GlobalAlertPreferences) error
}

// Severity rank ordering. Higher rank = more severe. A user with
// threshold "warning" receives warning, critical, and emergency alerts
// but not info.
var severityRank = map[string]int{
	SeverityInfo:      1,
	SeverityWarning:   2,
	SeverityCritical:  3,
	SeverityEmergency: 4,
}

// severityMeetsThreshold returns true if alertSeverity is at or above
// the user-defined threshold. Unknown values default to "warning"
// which matches normalizeSeverity.
func severityMeetsThreshold(alertSeverity, threshold string) bool {
	if threshold == "" {
		return true // no threshold = receive everything
	}
	alertRank, aOK := severityRank[normalizeSeverity(alertSeverity)]
	threshRank, tOK := severityRank[normalizeSeverity(threshold)]
	if !aOK || !tOK {
		return true // unknown severity: be permissive
	}
	return alertRank >= threshRank
}

// IsInQuietHours returns true if the given UTC time falls inside the
// user's quiet-hours window. Days, StartTime, EndTime, and Timezone
// are all evaluated. An empty QuietHours struct returns false (not in
// quiet hours).
func IsInQuietHours(now time.Time, qh QuietHours) bool {
	if len(qh.Days) == 0 {
		return false
	}

	// Resolve timezone. Fall back to UTC on parse error.
	loc := time.UTC
	if qh.Timezone != "" {
		l, err := time.LoadLocation(qh.Timezone)
		if err == nil {
			loc = l
		}
	}

	localNow := now.In(loc)
	weekday := DayOfWeek(localNow.Weekday())

	// Check if today is in the active days.
	dayMatch := false
	for _, d := range qh.Days {
		if d == weekday {
			dayMatch = true
			break
		}
	}
	if !dayMatch {
		return false
	}

	startMin, sErr := parseHHMM(qh.StartTime)
	endMin, eErr := parseHHMM(qh.EndTime)
	if sErr != nil || eErr != nil {
		return false
	}
	nowMin := localNow.Hour()*60 + localNow.Minute()

	if startMin <= endMin {
		// Simple window within a single day.
		return nowMin >= startMin && nowMin < endMin
	}
	// Overnight window: e.g. 22:00 -> 06:00.
	return nowMin >= startMin || nowMin < endMin
}

// parseHHMM parses "HH:MM" into total minutes past midnight. Returns
// an error on malformed input.
func parseHHMM(s string) (int, error) {
	if len(s) < 4 || s[2] != ':' {
		return 0, fmt.Errorf("invalid time %q", s)
	}
	h := 0
	m := 0
	for i, c := range s {
		if i == 2 {
			continue
		}
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid time %q", s)
		}
		d := int(c - '0')
		if i < 2 {
			h = h*10 + d
		} else {
			m = m*10 + d
		}
	}
	if h > 23 || m > 59 {
		return 0, fmt.Errorf("invalid time %q", s)
	}
	return h*60 + m, nil
}

// EvaluateResult describes why a user should or should not receive an
// alert.
type EvaluateResult struct {
	ShouldNotify   bool   `json:"should_notify"`
	Reason         string `json:"reason"`         // human-readable
	SuppressedBy   string `json:"suppressed_by,omitempty"` // "quiet_hours", "severity", "mute", "channel"
}

// Evaluate determines whether the given user should receive the given
// alert based on their preferences. The global preferences are used
// as fallback for any unset field. Channel-specific filtering is
// applied externally by the routing engine.
func Evaluate(prefs *UserAlertPreferences, alertSeverity string, now time.Time) EvaluateResult {
	if prefs == nil {
		return EvaluateResult{ShouldNotify: true, Reason: "no preferences configured"}
	}
	if prefs.MuteAll {
		return EvaluateResult{ShouldNotify: false, Reason: "all notifications muted", SuppressedBy: "mute"}
	}
	if !severityMeetsThreshold(alertSeverity, prefs.SeverityThreshold) {
		return EvaluateResult{
			ShouldNotify: false,
			Reason:       fmt.Sprintf("alert severity %q below threshold %q", alertSeverity, prefs.SeverityThreshold),
			SuppressedBy: "severity",
		}
	}
	if IsInQuietHours(now, prefs.QuietHours) {
		return EvaluateResult{
			ShouldNotify: false,
			Reason:       "within quiet hours window",
			SuppressedBy: "quiet_hours",
		}
	}
	return EvaluateResult{ShouldNotify: true, Reason: "preferences permit notification"}
}

// IsChannelEnabled returns true if the channel type is enabled in the
// user's preferences. If the user has no entry for the type, it
// defaults to true.
func (p *UserAlertPreferences) IsChannelEnabled(channelType string) bool {
	if p == nil || p.ChannelPreferences == nil {
		return true
	}
	enabled, ok := p.ChannelPreferences[channelType]
	if !ok {
		return true
	}
	return enabled
}

// MarshalQuietHours is a helper for JSON serialising quiet hours when
// stored in a jsonb column. Returns nil for empty struct.
func MarshalQuietHours(qh QuietHours) ([]byte, error) {
	if qh.StartTime == "" && qh.EndTime == "" && qh.Timezone == "" && len(qh.Days) == 0 {
		return nil, nil
	}
	return json.Marshal(qh)
}

// UnmarshalQuietHours is a helper for JSON deserialising quiet hours
// from a jsonb column. Returns a zero-valued QuietHours on nil input.
func UnmarshalQuietHours(data []byte) (QuietHours, error) {
	var qh QuietHours
	if len(data) == 0 {
		return qh, nil
	}
	if err := json.Unmarshal(data, &qh); err != nil {
		return qh, fmt.Errorf("alerts: decode quiet hours: %w", err)
	}
	return qh, nil
}

// ErrPreferencesNotFound is returned when no preferences record exists
// for the given user or org.
var ErrPreferencesNotFound = errors.New("alert preferences not found")
