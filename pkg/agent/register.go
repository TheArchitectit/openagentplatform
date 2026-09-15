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
// Field names and units must match the server's handleRegisterAgent
// contract: cpu_count (int), total_memory_mb (int64), total_disk_gb
// (int64), and the per-site registration token in agent_token.
type RegisterRequest struct {
	SiteID        string   `json:"site_id"`
	Hostname      string   `json:"hostname"`
	OS            string   `json:"os"`
	Platform      string   `json:"platform"`
	Arch          string   `json:"arch"`
	CPUCount      int      `json:"cpu_count"`
	TotalMemoryMB int64    `json:"total_memory_mb"`
	TotalDiskGB   int64    `json:"total_disk_gb"`
	AgentVersion  string   `json:"agent_version"`
	AgentToken    string   `json:"agent_token"`
	Tags          []string `json:"tags,omitempty"`
}

// RegisterResponse is what the API returns on successful registration.
// The server sends the session token under both "token" (canonical) and
// "auth_token" (legacy); either populates AuthToken.
type RegisterResponse struct {
	AgentID    string `json:"agent_id"`
	AuthToken  string `json:"auth_token"`
	Token      string `json:"token"`
	NATSURL    string `json:"nats_url,omitempty"`
	APIURL     string `json:"api_url,omitempty"`
	SigningKey string `json:"signing_key,omitempty"`
}

// effectiveToken returns the session token from whichever response key
// the server used.
func (rr *RegisterResponse) effectiveToken() string {
	if rr.AuthToken != "" {
		return rr.AuthToken
	}
	return rr.Token
}

// APIClient is a thin HTTP wrapper used for registration and other REST calls.
type APIClient struct {
	baseURL string
	token   string
	client  *http.Client
	log     *slog.Logger
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
	rr.AuthToken = rr.effectiveToken()
	if rr.AgentID == "" || rr.AuthToken == "" {
		return nil, fmt.Errorf("register response missing agent_id or auth_token")
	}
	return &rr, nil
}

// bytesToMB converts a byte count to whole megabytes (rounding up so the
// server never under-reports capacity).
func bytesToMB(b uint64) int64 {
	if b == 0 {
		return 0
	}
	return int64((b + (1 << 20) - 1) >> 20)
}

// bytesToGB converts a byte count to whole gigabytes (rounding up).
func bytesToGB(b uint64) int64 {
	if b == 0 {
		return 0
	}
	return int64((b + (1 << 30) - 1) >> 30)
}

// RegisterAgent is a convenience wrapper: it builds the request from host info
// and writes the resulting agent_id/token back to the config.
func RegisterAgent(ctx context.Context, cfg *Config, api *APIClient, hi *HostInfo, log *slog.Logger) error {
	if cfg.RegistrationToken == "" {
		return fmt.Errorf("no registration token configured (set agent_registration_token in config or AGENT_REGISTRATION_TOKEN in the environment)")
	}
	req := &RegisterRequest{
		SiteID:        cfg.SiteID,
		Hostname:      hi.Hostname,
		OS:            hi.OS,
		Platform:      hi.Platform,
		Arch:          hi.Arch,
		CPUCount:      hi.NumCPU,
		TotalMemoryMB: bytesToMB(hi.TotalMemory),
		TotalDiskGB:   bytesToGB(hi.TotalDisk),
		AgentVersion:  hi.AgentVersion,
		AgentToken:    cfg.RegistrationToken,
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
	if resp.SigningKey != "" {
		cfg.SigningKey = resp.SigningKey
	}
	if err := cfg.Save(); err != nil {
		log.Warn("failed to persist post-registration config", "err", err)
	}
	log.Info("agent registered", "agent_id", cfg.AgentID, "site_id", cfg.SiteID)
	return nil
}
