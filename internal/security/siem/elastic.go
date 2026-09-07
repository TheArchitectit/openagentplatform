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

// ElasticForwarder ships security events to an Elasticsearch Bulk API endpoint.
type ElasticForwarder struct {
	endpoint   string
	apiKey     string
	httpClient *http.Client
}

func NewElasticForwarder(endpoint, apiKey string) *ElasticForwarder {
	return &ElasticForwarder{
		endpoint:   endpoint,
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (f *ElasticForwarder) Name() models.SIEMType {
	return models.SIEMElastic
}

func (f *ElasticForwarder) BatchSize() int {
	return 100
}

func (f *ElasticForwarder) Flush(ctx context.Context, events []*models.SecurityEvent) error {
	var payload bytes.Buffer
	enc := json.NewEncoder(&payload)
	for _, ev := range events {
		action := map[string]any{"index": map[string]any{"_id": ev.ID}}
		if err := enc.Encode(action); err != nil {
			return err
		}
		if err := enc.Encode(ev); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, "POST", f.endpoint, &payload)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "ApiKey "+f.apiKey)
	req.Header.Set("Content-Type", "application/x-ndjson")
	resp, err := f.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("elastic: %s: %s", resp.Status, body)
	}
	return nil
}
