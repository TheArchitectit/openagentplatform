package models

import (
	"encoding/json"
	"fmt"
	"time"
)

type User struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	OrgID     string    `json:"org_id"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type Site struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"org_id"`
	Name      string    `json:"name"`
	Region    string    `json:"region"`
	CreatedAt time.Time `json:"created_at"`
}

type Agent struct {
	ID              string         `json:"id"`
	AgentID         string         `json:"agent_id"`
	SiteID          string         `json:"site_id"`
	ClientID        string         `json:"client_id,omitempty"`
	OrgID           string         `json:"org_id"`
	Hostname        string         `json:"hostname"`
	OperatingSystem string         `json:"os" db:"operating_system"`
	Arch            string         `json:"arch" db:"goarch"`
	Platform        string         `json:"platform"`
	CPUCount        int            `json:"cpu_count"`
	TotalMemoryMB   int64          `json:"total_memory_mb" db:"total_ram"`
	TotalDiskGB     int64          `json:"total_disk_gb"`
	Disks           map[string]any `json:"disks,omitempty"`
	Services        map[string]any `json:"services,omitempty"`
	WMIDetail       map[string]any `json:"wmi_detail,omitempty"`
	PublicIP        string         `json:"public_ip,omitempty"`
	BootTime        *time.Time     `json:"boot_time,omitempty"`
	LoggedInUser    string         `json:"logged_in_username,omitempty"`
	NeedsReboot     bool           `json:"needs_reboot"`
	Inventory       map[string]any `json:"inventory,omitempty"`
	MeshToken       string         `json:"mesh_token,omitempty"`
	Tags            []string       `json:"tags"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	AgentVersion    string         `json:"agent_version"`
	Version         string         `json:"version"`
	Status          string         `json:"status"`
	LastSeen        time.Time      `json:"last_seen"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       *time.Time     `json:"deleted_at,omitempty"`
}

// Heartbeat is the payload published by agents on oap.agents.<id>.heartbeat.
type Heartbeat struct {
	AgentID     string    `json:"agent_id"`
	Timestamp   time.Time `json:"timestamp"`
	CPUPercent  float64   `json:"cpu_percent"`
	MemPercent  float64   `json:"mem_percent"`
	DiskPercent float64   `json:"disk_percent"`
	UptimeSecs  uint64    `json:"uptime_secs"`
	Version     string    `json:"version"`
}

// UnmarshalJSON decodes a Heartbeat, accepting Timestamp as either an int64
// unix-seconds value (what pkg/agent publishes: time.Now().Unix()) or an
// RFC3339 string. Without this, json.Unmarshal rejects the numeric form and
// every agent heartbeat fails to decode server-side. Magnitude auto-detection
// also accepts milli/micro/nanosecond integers so future agent versions don't
// silently land in 1970.
func (h *Heartbeat) UnmarshalJSON(data []byte) error {
	// Shadow struct: same fields but Timestamp as RawMessage so time.Time's
	// strict UnmarshalJSON never runs on a numeric value, plus no recursion
	// into this method.
	type shadow struct {
		AgentID     string          `json:"agent_id"`
		Timestamp   json.RawMessage `json:"timestamp"`
		CPUPercent  float64         `json:"cpu_percent"`
		MemPercent  float64         `json:"mem_percent"`
		DiskPercent float64         `json:"disk_percent"`
		UptimeSecs  uint64          `json:"uptime_secs"`
		Version     string          `json:"version"`
	}
	var s shadow
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	ts := time.Time{}
	if len(s.Timestamp) > 0 && string(s.Timestamp) != "null" {
		var n int64
		if err := json.Unmarshal(s.Timestamp, &n); err == nil {
			ts = unixSecondsToTime(n)
		} else {
			var str string
			if err2 := json.Unmarshal(s.Timestamp, &str); err2 == nil {
				if parsed, err3 := time.Parse(time.RFC3339, str); err3 == nil {
					ts = parsed
				}
			}
			// Neither number nor parseable string: keep zero time; the
			// heartbeat handler already substitutes time.Now() for zero.
		}
	}

	*h = Heartbeat{
		AgentID:     s.AgentID,
		Timestamp:   ts,
		CPUPercent:  s.CPUPercent,
		MemPercent:  s.MemPercent,
		DiskPercent: s.DiskPercent,
		UptimeSecs:  s.UptimeSecs,
		Version:     s.Version,
	}
	return nil
}

// unixSecondsToTime interprets an integer timestamp by magnitude:
// seconds (~1.7e9), milliseconds (~1.7e12), microseconds (~1.7e15), or
// nanoseconds (~1.7e18). Values far from any plausible epoch resolve to
// their literal second interpretation rather than erroring.
func unixSecondsToTime(n int64) time.Time {
	switch {
	case n == 0:
		return time.Time{}
	case n > 1e17: // nanoseconds
		return time.Unix(0, n)
	case n > 1e14: // microseconds
		return time.UnixMicro(n)
	case n > 1e11: // milliseconds
		return time.UnixMilli(n)
	default: // seconds
		return time.Unix(n, 0)
	}
}

// String renders a Heartbeat compactly for logs.
func (h Heartbeat) String() string {
	return fmt.Sprintf("Heartbeat{agent=%s ts=%s cpu=%.1f mem=%.1f disk=%.1f v=%s}",
		h.AgentID, h.Timestamp.Format(time.RFC3339), h.CPUPercent, h.MemPercent, h.DiskPercent, h.Version)
}
