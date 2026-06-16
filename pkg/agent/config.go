package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"sigs.k8s.io/yaml"
)

// Config holds all agent runtime configuration.
type Config struct {
	SiteID    string `json:"site_id"     yaml:"site_id"`
	AgentID   string `json:"agent_id"    yaml:"agent_id"`
	AuthToken string `json:"auth_token"  yaml:"auth_token"`

	NATSURL    string `json:"nats_url"    yaml:"nats_url"`
	NATSCAFile string `json:"nats_ca"     yaml:"nats_ca"`
	NATSCert   string `json:"nats_cert"   yaml:"nats_cert"`
	NATSKey    string `json:"nats_key"    yaml:"nats_key"`

	APIURL  string `json:"api_url"  yaml:"api_url"`
	APIInsec bool   `json:"api_insecure" yaml:"api_insecure"`

	ConfigPath string `json:"config_path" yaml:"-"`

	LogLevel string `json:"log_level" yaml:"log_level"`

	HeartbeatIntervalSec int `json:"heartbeat_interval_sec" yaml:"heartbeat_interval_sec"`

	ScriptTimeoutSec int `json:"script_timeout_sec" yaml:"script_timeout_sec"`
}

// DefaultConfigPath returns the OS-appropriate config file path.
func DefaultConfigPath() string {
	switch runtime.GOOS {
	case "windows":
		if p := os.Getenv("PROGRAMDATA"); p != "" {
			return filepath.Join(p, "OpenAgentPlatform", "agent.yaml")
		}
		return filepath.Join(`C:\ProgramData\OpenAgentPlatform`, "agent.yaml")
	case "darwin":
		return "/etc/openagentplatform/agent.yaml"
	default:
		return "/etc/openagentplatform/agent.yaml"
	}
}

func defaultAPIURL() string {
	return "https://api.openagentplatform.local"
}

func defaultNATSURL() string {
	return "tls://nats.openagentplatform.local:4222"
}

// LoadConfig reads config from a file (YAML or JSON) and applies env overrides.
// If path is empty, the OS default is used. A missing file is not an error;
// defaults plus env vars are returned.
func LoadConfig(path string) (*Config, error) {
	if path == "" {
		path = DefaultConfigPath()
	}

	c := &Config{
		APIURL:               defaultAPIURL(),
		NATSURL:              defaultNATSURL(),
		LogLevel:             "info",
		HeartbeatIntervalSec: 60,
		ScriptTimeoutSec:     300,
		ConfigPath:           path,
	}

	if path != "" {
		if data, err := os.ReadFile(path); err == nil {
			if jerr := json.Unmarshal(data, c); jerr != nil {
				if yerr := yaml.Unmarshal(data, c); yerr != nil {
					return nil, fmt.Errorf("config %s: not valid json (%v) or yaml (%v)", path, jerr, yerr)
				}
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read config %s: %w", path, err)
		}
	}

	applyEnv(c)
	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func applyEnv(c *Config) {
	if v := os.Getenv("AGENT_SITE_ID"); v != "" {
		c.SiteID = v
	}
	if v := os.Getenv("AGENT_AGENT_ID"); v != "" {
		c.AgentID = v
	}
	if v := os.Getenv("AGENT_TOKEN"); v != "" {
		c.AuthToken = v
	}
	if v := os.Getenv("AGENT_AUTH_TOKEN"); v != "" {
		c.AuthToken = v
	}
	if v := os.Getenv("NATS_URL"); v != "" {
		c.NATSURL = v
	}
	if v := os.Getenv("NATS_CA_FILE"); v != "" {
		c.NATSCAFile = v
	}
	if v := os.Getenv("NATS_CERT_FILE"); v != "" {
		c.NATSCert = v
	}
	if v := os.Getenv("NATS_KEY_FILE"); v != "" {
		c.NATSKey = v
	}
	if v := os.Getenv("API_URL"); v != "" {
		c.APIURL = v
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		c.LogLevel = v
	}
}

func (c *Config) validate() error {
	var missing []string
	if strings.TrimSpace(c.APIURL) == "" {
		missing = append(missing, "API_URL")
	}
	if strings.TrimSpace(c.NATSURL) == "" {
		missing = append(missing, "NATS_URL")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required config: %s", strings.Join(missing, ", "))
	}
	if c.HeartbeatIntervalSec <= 0 {
		c.HeartbeatIntervalSec = 60
	}
	if c.ScriptTimeoutSec <= 0 {
		c.ScriptTimeoutSec = 300
	}
	return nil
}

// Save writes the config to disk in JSON form. Used to persist the
// agent_id and auth_token returned by the registration endpoint.
func (c *Config) Save() error {
	if c.ConfigPath == "" {
		c.ConfigPath = DefaultConfigPath()
	}
	if err := os.MkdirAll(filepath.Dir(c.ConfigPath), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := c.ConfigPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := os.Rename(tmp, c.ConfigPath); err != nil {
		return fmt.Errorf("rename config: %w", err)
	}
	return nil
}
