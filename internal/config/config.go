package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPPort      string
	Env           string
	LogLevel      string

	PostgresDSN   string
	NATSURL       string
	NATSCertFile  string
	NATSKeyFile   string
	NATSCAFile    string

	OIDCIssuerURL    string
	OIDCClientID     string
	OIDCClientSecret string
	OIDCRedirectURL  string

	SessionIssuer   string
	SessionAudience string
	SessionKeyPath  string

	CookieDomain  string
	CookieSecure  bool

	SentryDSN     string

	// PolicyEvalInterval is the interval at which the policy engine
	// runs a full sweep across all agents. Defaults to 5 minutes.
	PolicyEvalInterval time.Duration

	// OzoreAI — hosted LLM agent provider (OpenAI-compatible).
	// The API key (OZORE_API_KEY) is read by the Python adapter
	// directly from the environment and is NOT stored in this struct.
	OzoreModel   string
	OzoreBaseURL string
}

func Load() (*Config, error) {
	c := &Config{
		HTTPPort:        getEnv("HTTP_PORT", "8080"),
		Env:             getEnv("APP_ENV", "development"),
		LogLevel:        getEnv("LOG_LEVEL", "info"),
		PostgresDSN:     os.Getenv("POSTGRES_DSN"),
		NATSURL:         getEnv("NATS_URL", "nats://localhost:4222"),
		NATSCertFile:    os.Getenv("NATS_CERT_FILE"),
		NATSKeyFile:     os.Getenv("NATS_KEY_FILE"),
		NATSCAFile:      os.Getenv("NATS_CA_FILE"),
		OIDCIssuerURL:    os.Getenv("OIDC_ISSUER_URL"),
		OIDCClientID:     os.Getenv("OIDC_CLIENT_ID"),
		OIDCClientSecret: os.Getenv("OIDC_CLIENT_SECRET"),
		OIDCRedirectURL:  getEnv("OIDC_REDIRECT_URL", "http://localhost:8080/auth/callback"),
		SessionIssuer:    getEnv("SESSION_ISSUER", "openagentplatform"),
		SessionAudience:  getEnv("SESSION_AUDIENCE", "oap-web"),
		SessionKeyPath:   os.Getenv("SESSION_KEY_PATH"),
		CookieDomain:     getEnv("COOKIE_DOMAIN", "localhost"),
		CookieSecure:     getEnv("COOKIE_SECURE", "false") == "true",
		SentryDSN:        os.Getenv("SENTRY_DSN"),
		PolicyEvalInterval: getDurationEnv("POLICY_EVAL_INTERVAL", 5*time.Minute),
		OzoreModel:         getEnv("OZORE_MODEL", "ozore/custom"),
		OzoreBaseURL:       getEnv("OZORE_BASE_URL", "https://ozore.com/v1"),
	}

	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) validate() error {
	var missing []string
	if c.PostgresDSN == "" {
		missing = append(missing, "POSTGRES_DSN")
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required env vars: %s", strings.Join(missing, ", "))
	}
	if c.Env != "development" && c.Env != "staging" && c.Env != "production" {
		return errors.New("APP_ENV must be one of: development, staging, production")
	}
	return nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// getDurationEnv reads a duration from an env var, falling back to the
// provided default if missing or invalid.
func getDurationEnv(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	if d, err := time.ParseDuration(v); err == nil {
		return d
	}
	// Accept raw seconds as a fallback for shell-friendliness.
	if secs, err := strconv.Atoi(v); err == nil {
		return time.Duration(secs) * time.Second
	}
	return fallback
}
