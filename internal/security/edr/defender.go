package edr

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

type DefenderProvider struct {
	webhookSecret string
	tenantID      string
	clientID      string
	clientSecret  string
	httpClient    *http.Client
	token         string
	tokenExp      time.Time
}

func NewDefenderProvider(tenantID, clientID, clientSecret, webhookSecret string) *DefenderProvider {
	return &DefenderProvider{
		tenantID:      tenantID,
		clientID:      clientID,
		clientSecret:  clientSecret,
		webhookSecret: webhookSecret,
		httpClient:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *DefenderProvider) Name() models.EDRProviderType {
	return models.EDRDefender
}

func (p *DefenderProvider) VerifyWebhookSignature(payload []byte, signature string) bool {
	if p.webhookSecret == "" {
		return true
	}
	return verifyHMAC(payload, signature, p.webhookSecret)
}

type defenderAlert struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Severity    string    `json:"severity"`
	Category    string    `json:"category"`
	MITRE       struct {
		Tactic    string `json:"tactic"`
		Technique string `json:"technique"`
	} `json:"mitreTechniques"`
	MachineID  string    `json:"machineId"`
	DetectedAt time.Time `json:"detectionDateTime"`
}

func (p *DefenderProvider) ParseWebhookEvent(payload []byte) (*models.SecurityEvent, error) {
	var raw struct {
		Value []defenderAlert `json:"value"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, err
	}
	if len(raw.Value) == 0 {
		return nil, fmt.Errorf("defender: empty alert array")
	}
	a := raw.Value[0]
	severity := "info"
	switch a.Severity {
	case "High":
		severity = "critical"
	case "Medium":
		severity = "warning"
	}
	return &models.SecurityEvent{
		Provider:        models.EDRDefender,
		ProviderEventID: a.ID,
		Severity:        severity,
		Tactic:          a.MITRE.Tactic,
		Technique:       a.MITRE.Technique,
		DetectionType:   a.Category,
		OccurredAt:      a.DetectedAt,
		IngestionMethod: "webhook",
		Payload: map[string]any{
			"machine_id": a.MachineID,
			"title":      a.Title,
		},
	}, nil
}

func (p *DefenderProvider) PullRecentEvents(ctx context.Context, since time.Time) ([]*models.SecurityEvent, error) {
	if err := p.refreshToken(ctx); err != nil {
		return nil, err
	}
	url := fmt.Sprintf("https://api.security.microsoft.com/api/alerts?$filter=detectionDateTime ge %s", since.UTC().Format(time.RFC3339))
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+p.token)
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("defender: %s: %s", resp.Status, body)
	}
	body, _ := io.ReadAll(resp.Body)
	ev, err := p.ParseWebhookEvent(body)
	if err != nil {
		return nil, nil
	}
	if ev != nil {
		return []*models.SecurityEvent{ev}, nil
	}
	return nil, nil
}

func (p *DefenderProvider) refreshToken(ctx context.Context) error {
	if p.token != "" && time.Now().Before(p.tokenExp) {
		return nil
	}
	url := "https://login.microsoftonline.com/" + p.tenantID + "/oauth2/v2.0/token"
	body := []byte("client_id=" + p.clientID + "&client_secret=" + p.clientSecret +
		"&scope=https://api.security.microsoft.com/.default&grant_type=client_credentials")
	req, _ := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := p.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	p.token = out.AccessToken
	p.tokenExp = time.Now().Add(time.Duration(out.ExpiresIn-60) * time.Second)
	return nil
}
