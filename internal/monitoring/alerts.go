package monitoring

import (
	"errors"
	"sort"
	"sync"
	"time"
)

const (
	AlertOpen         = "open"
	AlertAcknowledged = "acknowledged"
	AlertSnoozed      = "snoozed"
	AlertResolved     = "resolved"
)

var (
	ErrAlertNotFound          = errors.New("monitoring: alert not found")
	ErrInvalidAlertTransition = errors.New("monitoring: invalid alert transition")
)

// Alert is a monitoring alert exposed to dashboard clients.
type Alert struct {
	ID             string     `json:"id"`
	Source         string     `json:"source"`
	Severity       string     `json:"severity"`
	State          string     `json:"state"`
	Message        string     `json:"message"`
	AcknowledgedBy string     `json:"acknowledged_by,omitempty"`
	SnoozedUntil   *time.Time `json:"snoozed_until,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
}

// AlertFilter selects alerts by state, severity, or source.
type AlertFilter struct{ State, Severity, Source string }

// AlertManager stores and manages the alert lifecycle. It is safe for concurrent use.
type AlertManager struct {
	mu     sync.RWMutex
	alerts map[string]Alert
	now    func() time.Time
}

func NewAlertManager() *AlertManager {
	return &AlertManager{alerts: make(map[string]Alert), now: time.Now}
}

func (m *AlertManager) Add(alert Alert) error {
	if alert.ID == "" {
		return errors.New("monitoring: alert id required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.alerts == nil {
		m.alerts = make(map[string]Alert)
	}
	if _, ok := m.alerts[alert.ID]; ok {
		return errors.New("monitoring: alert already exists")
	}
	now := m.clock()
	if alert.State == "" {
		alert.State = AlertOpen
	}
	if alert.CreatedAt.IsZero() {
		alert.CreatedAt = now
	}
	if alert.UpdatedAt.IsZero() {
		alert.UpdatedAt = now
	}
	m.alerts[alert.ID] = cloneAlert(alert)
	return nil
}

func (m *AlertManager) List(filter AlertFilter) []Alert {
	m.mu.RLock()
	defer m.mu.RUnlock()
	alerts := make([]Alert, 0, len(m.alerts))
	for _, alert := range m.alerts {
		if filter.State != "" && alert.State != filter.State {
			continue
		}
		if filter.Severity != "" && alert.Severity != filter.Severity {
			continue
		}
		if filter.Source != "" && alert.Source != filter.Source {
			continue
		}
		alerts = append(alerts, cloneAlert(alert))
	}
	sort.Slice(alerts, func(i, j int) bool {
		if alerts[i].CreatedAt.Equal(alerts[j].CreatedAt) {
			return alerts[i].ID < alerts[j].ID
		}
		return alerts[i].CreatedAt.After(alerts[j].CreatedAt)
	})
	return alerts
}

func (m *AlertManager) Acknowledge(id, actor string) (Alert, error) {
	return m.update(id, func(alert *Alert, now time.Time) error {
		if alert.State != AlertOpen && alert.State != AlertSnoozed {
			return ErrInvalidAlertTransition
		}
		alert.State, alert.AcknowledgedBy, alert.SnoozedUntil = AlertAcknowledged, actor, nil
		alert.UpdatedAt = now
		return nil
	})
}

func (m *AlertManager) Resolve(id string) (Alert, error) {
	return m.update(id, func(alert *Alert, now time.Time) error {
		if alert.State == AlertResolved {
			return ErrInvalidAlertTransition
		}
		alert.State, alert.SnoozedUntil, alert.ResolvedAt, alert.UpdatedAt = AlertResolved, nil, &now, now
		return nil
	})
}

func (m *AlertManager) Snooze(id string, duration time.Duration) (Alert, error) {
	if duration <= 0 {
		return Alert{}, errors.New("monitoring: snooze duration must be positive")
	}
	return m.update(id, func(alert *Alert, now time.Time) error {
		if alert.State == AlertResolved {
			return ErrInvalidAlertTransition
		}
		until := now.Add(duration)
		alert.State, alert.SnoozedUntil, alert.UpdatedAt = AlertSnoozed, &until, now
		return nil
	})
}

func (m *AlertManager) update(id string, fn func(*Alert, time.Time) error) (Alert, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	alert, ok := m.alerts[id]
	if !ok {
		return Alert{}, ErrAlertNotFound
	}
	if err := fn(&alert, m.clock()); err != nil {
		return Alert{}, err
	}
	m.alerts[id] = alert
	return cloneAlert(alert), nil
}

func (m *AlertManager) clock() time.Time {
	if m.now != nil {
		return m.now()
	}
	return time.Now()
}
func cloneAlert(alert Alert) Alert {
	if alert.SnoozedUntil != nil {
		value := *alert.SnoozedUntil
		alert.SnoozedUntil = &value
	}
	if alert.ResolvedAt != nil {
		value := *alert.ResolvedAt
		alert.ResolvedAt = &value
	}
	return alert
}
