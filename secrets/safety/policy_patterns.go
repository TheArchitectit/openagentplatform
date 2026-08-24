package safety

import (
	"regexp"
	"strings"
)

// envPrefix is the allowed prefix for injected secrets in env vars.
const envPrefix = "OAP_INJECTED_"

// secretNamePatterns are substrings in env var names that suggest a secret.
var secretNamePatterns = []string{
	"PASSWORD", "PASSWD", "SECRET", "TOKEN", "API_KEY", "APIKEY",
	"PRIVATE_KEY", "PRIVATEKEY", "CREDENTIAL", "AUTH", "SESSION",
	"ENCRYPTION_KEY", "SIGNING_KEY", "CLIENT_SECRET",
}

// secretValuePatterns matches common credential value formats.
var secretValuePatterns = []*regexp.Regexp{
	regexp.MustCompile(`^-----BEGIN .+ PRIVATE KEY-----`),          // PEM keys
	regexp.MustCompile(`^[A-Za-z0-9_-]{40,}\.[A-Za-z0-9_-]{40,}`),  // JWT-like
	regexp.MustCompile(`^(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{36,}$`), // GitHub tokens
	regexp.MustCompile(`^xox[bpoasr]-[A-Za-z0-9-]{10,}$`),          // Slack tokens
	regexp.MustCompile(`^sk-[A-Za-z0-9]{20,}$`),                    // OpenAI-style keys
	regexp.MustCompile(`^AKIA[0-9A-Z]{16}$`),                       // AWS access keys
	regexp.MustCompile(`^AIza[0-9A-Za-z_-]{35}$`),                  // Google API keys
	regexp.MustCompile(`^glpat-[A-Za-z0-9_-]{20,}$`),               // GitLab PAT
	regexp.MustCompile(`^[A-Za-z0-9+/]{64,}={0,2}$`),               // base64 blobs >= 64 chars (reduces false positives)
}

// refPattern matches a secret reference URI like ref:oap://...
var refPattern = regexp.MustCompile(`^ref:oap://`)

// isSecretEnvVarName reports whether the name suggests a secret value.
func isSecretEnvVarName(name string) bool {
	upper := strings.ToUpper(name)
	for _, pattern := range secretNamePatterns {
		if strings.Contains(upper, pattern) {
			return true
		}
	}
	return false
}

// envPrefixAllowed reports whether the env var name has the OAP_INJECTED_ prefix.
func envPrefixAllowed(name string) bool {
	return strings.HasPrefix(name, envPrefix)
}

// hasSecretValue reports whether the value looks like a raw secret (not a ref).
func hasSecretValue(name, value string) bool {
	if value == "" {
		return false
	}
	// A reference URI is not a raw secret value.
	if refPattern.MatchString(value) {
		return false
	}
	for _, re := range secretValuePatterns {
		if re.MatchString(value) {
			return true
		}
	}
	// If the name suggests a secret and the value is non-trivial, consider it a secret.
	if isSecretEnvVarName(name) && len(value) >= 16 {
		return true
	}
	return false
}

// matchSecretPattern returns the matching rule name and true if the argument
// looks like a secret value.
func matchSecretPattern(arg string) (string, bool) {
	if arg == "" {
		return "", false
	}
	if refPattern.MatchString(arg) {
		return "", false
	}
	for i, re := range secretValuePatterns {
		if re.MatchString(arg) {
			ruleNames := []string{
				"pem_private_key", "jwt_token", "github_token", "slack_token",
				"openai_key", "aws_access_key", "google_api_key", "gitlab_pat",
				"base64_blob",
			}
			return ruleNames[i], true
		}
	}
	return "", false
}

// looksLikeSecret is a top-level helper used by ScriptCredentialSafe to
// quickly check whether a key-value pair looks like a secret.
func looksLikeSecret(key, value string) bool {
	if value == "" {
		return false
	}
	if refPattern.MatchString(value) {
		return false
	}
	if isSecretEnvVarName(key) {
		return true
	}
	_, matched := matchSecretPattern(value)
	return matched
}

// redactValue truncates a value for safe logging, showing only the first 4
// characters (or fewer for very short values) followed by a redaction marker.
// Values shorter than 8 characters are fully redacted to avoid leaking
// short secrets like PINs or short tokens.
func redactValue(v string) string {
	if len(v) < 8 {
		return "***"
	}
	return v[:4] + "***"
}
