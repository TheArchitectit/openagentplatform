package siem

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// GenericForwarder ships security events to any HTTP SIEM endpoint,
// formatting each event as CEF, LEEF, or raw JSON.
type GenericForwarder struct {
	endpoint   string
	format     string // "cef" | "leef" | "json"
	token      string
	httpClient *http.Client
}

func NewGenericForwarder(endpoint, format, token string) *GenericForwarder {
	return &GenericForwarder{
		endpoint:   endpoint,
		format:     format,
		token:      token,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (f *GenericForwarder) Name() models.SIEMType {
	return models.SIEMGeneric
}

func (f *GenericForwarder) BatchSize() int {
	return 100
}

func (f *GenericForwarder) Flush(ctx context.Context, events []*models.SecurityEvent) error {
	var payload bytes.Buffer
	for _, ev := range events {
		switch f.format {
		case "cef":
			payload.WriteString(f.toCEF(ev))
		case "leef":
			payload.WriteString(f.toLEEF(ev))
		default:
			b, _ := json.Marshal(ev)
			payload.Write(b)
		}
		payload.WriteByte('\n')
	}
	req, err := http.NewRequestWithContext(ctx, "POST", f.endpoint, &payload)
	if err != nil {
		return err
	}
	if f.token != "" {
		req.Header.Set("Authorization", "Bearer "+f.token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("generic siem: %s: %s", resp.Status, body)
	}
	return nil
}

func (f *GenericForwarder) toCEF(ev *models.SecurityEvent) string {
	return fmt.Sprintf("CEF:0|OpenAgentPlatform|security|1.0|%s|%s|%d|src=oap dst=%s cs1Label=tactic cs1=%s cs2Label=technique cs2=%s\n",
		ev.ProviderEventID, ev.DetectionType, severityToInt(ev.Severity), ev.AgentID, ev.Tactic, ev.Technique)
}

func (f *GenericForwarder) toLEEF(ev *models.SecurityEvent) string {
	var sb strings.Builder
	sb.WriteString("LEEF:1.0|OpenAgentPlatform|security|1.0|")
	sb.WriteString(ev.ProviderEventID)
	sb.WriteString("|")
	sb.WriteString(fmt.Sprintf("devTime=%s sev=%d src=oap dst=%s cat=%s",
		ev.OccurredAt.UTC().Format(time.RFC3339), severityToInt(ev.Severity), ev.AgentID, ev.Tactic))
	sb.WriteString("\n")
	return sb.String()
}

func severityToInt(s string) int {
	switch s {
	case "critical":
		return 10
	case "warning":
		return 5
	default:
		return 1
	}
}
