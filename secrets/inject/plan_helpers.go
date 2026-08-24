package inject

import (
	"encoding/json"
	"time"

	"github.com/openagentplatform/openagentplatform/secrets"
)

// defaultTTL is used when the resolved secret does not specify its own.
const defaultTTL = 5 * time.Minute

// pickMethod decides the best delivery method for a resolved secret based on
// the path, key, and whether the backend issued a dynamic lease.
func pickMethod(agentID string, val *secrets.SecretValue) InjectMethod {
	if val == nil {
		return MethodEnv
	}
	// Dynamic secrets default to stdin — short-lived, one-shot use.
	if val.Metadata.IsDynamic && val.Metadata.LeaseDuration > 0 {
		return MethodStdin
	}
	// SSH keys and certificates are file-based.
	key := extractKey(val, val.Path)
	if isFileType(key) {
		return MethodFile
	}
	return MethodEnv
}

// encodeValue serialises a SecretValue to bytes suitable for injection.
// If the value has a single "value" key the raw string is used; otherwise the
// full data map is JSON-encoded.
func encodeValue(val *secrets.SecretValue) ([]byte, error) {
	if v, ok := val.Data["value"]; ok {
		if s, ok := v.(string); ok {
			return []byte(s), nil
		}
	}
	return jsonMarshal(val.Data)
}

// extractKey derives a short key name from the secret value or fallback URI.
func extractKey(val *secrets.SecretValue, fallback string) string {
	if val != nil {
		if k, ok := val.Data["key"].(string); ok && k != "" {
			return k
		}
	}
	// Use the last path segment as a default key name.
	parts := splitLast(fallback, '/')
	if parts != "" {
		return sanitizeKey(parts)
	}
	return "secret"
}

// isFileType returns true for credentials that should be written to a file
// (SSH keys, certificates, TLS bundles).
func isFileType(key string) bool {
	k := key
	if len(k) > 3 && (k[len(k)-4:] == "_key" || k[len(k)-4:] == "_pem") {
		return true
	}
	switch k {
	case "ssh-private-key", "ssh_key", "private_key", "tls-cert", "certificate", "cert", "ca-bundle":
		return true
	}
	return false
}

// backendFromURI extracts the backend type from a ref:oap:// URI.
func backendFromURI(uri string) string {
	const prefix = "ref:oap://"
	if len(uri) <= len(prefix) {
		return ""
	}
	rest := uri[len(prefix):]
	for i := 0; i < len(rest); i++ {
		if rest[i] == '/' {
			return rest[:i]
		}
	}
	return rest
}

// splitLast returns the text after the final '/' in s, or s if none.
func splitLast(s string, sep byte) string {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == sep {
			return s[i+1:]
		}
	}
	return s
}

// sanitizeKey converts an arbitrary string into a safe env-var suffix.
func sanitizeKey(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
			out = append(out, c)
		case c >= 'a' && c <= 'z':
			out = append(out, c-32) // uppercase
		case c >= '0' && c <= '9':
			out = append(out, c)
		case c == '-' || c == '_':
			out = append(out, '_')
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}

// jsonMarshal is a small wrapper around encoding/json to keep the import
// surface clean and allow future customisation.
func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}
