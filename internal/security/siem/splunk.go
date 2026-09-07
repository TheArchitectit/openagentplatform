package siem

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// SplunkForwarder ships security events to a Splunk HEC endpoint.
type SplunkForwarder struct {
	endpoint   string
	hecToken   string
	httpClient *http.Client
}

func NewSplunkForwarder(endpoint, hecToken string) *SplunkForwarder {
	return &SplunkForwarder{
		endpoint:   endpoint,
		hecToken:   hecToken,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (f *SplunkForwarder) Name() models.SIEMType {
	return models.SIEMSplunk
}

func (f *SplunkForwarder) BatchSize() int {
	return 100
}

type splunkEvent struct {
	Time   int64           `json:"time"`
	Host   string          `json:"host"`
	Source string          `json:"source"`
	Event  json.RawMessage `json:"event"`
}

func (f *SplunkForwarder) Flush(ctx context.Context, events []*models.SecurityEvent) error {
	var payload bytes.Buffer
	for _, ev := range events {
		secs := ev.OccurredAt.Unix()
		if secs == 0 {
			secs = time.Now().Unix()
		}
		eventBody, _ := json.Marshal(map[string]any{
			"severity":  ev.Severity,
			"provider":  ev.Provider,
			"agent_id":  ev.AgentID,
			"tactic":    ev.Tactic,
			"technique": ev.Technique,
		})
		se := splunkEvent{
			Time:   secs,
			Host:   "oap",
			Source: "oap.security_events",
			Event:  eventBody,
		}
		line, _ := json.Marshal(se)
		payload.Write(line)
		payload.WriteByte('\n')
	}
	req, err := http.NewRequestWithContext(ctx, "POST", f.endpoint, &payload)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Splunk "+f.hecToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("splunk: %s: %s", resp.Status, body)
	}
	return nil
}
