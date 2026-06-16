package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// RegisterRequest is the JSON body POSTed to /api/v1/agents/register.
type RegisterRequest struct {
	SiteID      string   `json:"site_id"`
	Hostname    string   `json:"hostname"`
	OS          string   `json:"os"`
	Platform    string   `json:"platform"`
	Arch        string   `json:"arch"`
	NumCPU      int      `json:"num_cpu"`
	TotalMemory uint64   `json:"total_memory"`
	TotalDisk   uint64   `json:"total_disk"`
	AgentVersion string  `json:"agent_version"`
	Tags        []string `json:"tags,omitempty"`
}

// RegisterResponse is what the API returns on successful registration.
type RegisterResponse struct {
	AgentID   string `json:"agent_id"`
	AuthToken string `json:"auth_token"`
	NATSURL   string `json:"nats_url,omitempty"`
	APIURL    string `json:"api_url,omitempty"`
}

// APIClient is a thin HTTP wrapper used for registration and other REST calls.
type APIClient struct {
	baseURL  string
	token    string
	client   *http.Client
	log      *slog.Logger
}

// NewAPIClient builds an APIClient.
func NewAPIClient(baseURL, token string, insecure bool, log *slog.Logger) *APIClient {
	t := &http.Transport{
		TLSHandshakeTimeout: 10 * time.Second,
		IdleConnTimeout:     90 * time.Second,
	}
	_ = insecure // TLS skip-verify is intentionally not exposed to keep mTLS strict
	return &APIClient{
		baseURL: baseURL,
		token:   token,
		client:  &http.Client{Transport: t, Timeout: 30 * time.Second},
		log:     log,
	}
}

// Register performs the registration flow. On success the returned
// RegisterResponse contains the agent_id and auth_token that should be
// persisted to the local config.
func (a *APIClient) Register(ctx context.Context, req *RegisterRequest) (*RegisterResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal register request: %w", err)
	}
	url := a.baseURL + "/api/v1/agents/register"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if a.token != "" {
		httpReq.Header.Set("Authorization", "Bearer "+a.token)
	}

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("register http call: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("register failed: %s: %s", resp.Status, string(respBody))
	}

	var rr RegisterResponse
	if err := json.Unmarshal(respBody, &rr); err != nil {
		return nil, fmt.Errorf("decode register response: %w", err)
	}
	if rr.AgentID == "" || rr.AuthToken == "" {
		return nil, fmt.Errorf("register response missing agent_id or auth_token")
	}
	return &rr, nil
}

// RegisterAgent is a convenience wrapper: it builds the request from host info
// and writes the resulting agent_id/token back to the config.
func RegisterAgent(ctx context.Context, cfg *Config, api *APIClient, hi *HostInfo, log *slog.Logger) error {
	req := &RegisterRequest{
		SiteID:       cfg.SiteID,
		Hostname:     hi.Hostname,
		OS:           hi.OS,
		Platform:     hi.Platform,
		Arch:         hi.Arch,
		NumCPU:       hi.NumCPU,
		TotalMemory:  hi.TotalMemory,
		TotalDisk:    hi.TotalDisk,
		AgentVersion: hi.AgentVersion,
	}

	resp, err := api.Register(ctx, req)
	if err != nil {
		return err
	}

	cfg.AgentID = resp.AgentID
	cfg.AuthToken = resp.AuthToken
	if resp.NATSURL != "" {
		cfg.NATSURL = resp.NATSURL
	}
	if resp.APIURL != "" {
		cfg.APIURL = resp.APIURL
	}
	if err := cfg.Save(); err != nil {
		log.Warn("failed to persist post-registration config", "err", err)
	}
	log.Info("agent registered", "agent_id", cfg.AgentID, "site_id", cfg.SiteID)
	return nil
}
