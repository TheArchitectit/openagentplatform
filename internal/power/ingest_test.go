package power

import (
	"context"
	"testing"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

func TestMapTransitionToEventType(t *testing.T) {
	cases := []struct {
		name      string
		source    string
		transition string
		percent   *int
		want      models.PowerEventType
	}{
		{"ups on_battery", "ups", "on_battery", nil, models.PowerEventOnBattery},
		{"ups on_line", "ups", "on_line", nil, models.PowerEventOnLine},
		{"battery discharging 50", "battery", "discharging", intPtr(50), models.PowerEventDischarging},
		{"battery discharging 15", "battery", "discharging", intPtr(15), models.PowerEventLowBattery},
		{"battery charging", "battery", "charging", nil, models.PowerEventCharging},
		{"battery full", "battery", "full", nil, models.PowerEventFull},
		{"critical", "battery", "critical", nil, models.PowerEventBatteryCrit},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := mapTransitionToEventType(models.PowerSource(c.source), c.transition, c.percent)
			if got != c.want {
				t.Errorf("mapTransitionToEventType(%q, %q, %v) = %q, want %q", c.source, c.transition, c.percent, got, c.want)
			}
		})
	}
}

func intPtr(i int) *int { return &i }

type captureSink struct {
	events []PowerEvent
}

func (c *captureSink) PublishPowerEvent(_ context.Context, e PowerEvent) {
	c.events = append(c.events, e)
}

func TestIngestorHandleResultEmitsOnTransition(t *testing.T) {
	sink := &captureSink{}
	ing := NewIngestor(nil, sink, nil)
	payload := []byte(`{"source":"ups","power_transition":"on_battery","previous_status":"OL","current_status":"OB","battery_percent":85}`)
	ing.HandleResult(context.Background(), "org1", "agent-1", "check-1", payload)
	if len(sink.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(sink.events))
	}
	if sink.events[0].EventType != models.PowerEventOnBattery {
		t.Errorf("EventType = %q, want on_battery", sink.events[0].EventType)
	}
	if sink.events[0].Source != models.PowerSourceUPS {
		t.Errorf("Source = %q, want ups", sink.events[0].Source)
	}
}

func TestIngestorHandleResultIgnoresEmpty(t *testing.T) {
	sink := &captureSink{}
	ing := NewIngestor(nil, sink, nil)
	ing.HandleResult(context.Background(), "org1", "agent-1", "check-1", nil)
	ing.HandleResult(context.Background(), "org1", "agent-1", "check-1", []byte(`{}`))
	ing.HandleResult(context.Background(), "org1", "agent-1", "check-1", []byte(`not json`))
	if len(sink.events) != 0 {
		t.Errorf("expected 0 events, got %d", len(sink.events))
	}
}
