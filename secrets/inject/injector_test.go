package inject

import (
	"testing"

	"github.com/openagentplatform/openagentplatform/secrets"
)

func TestPickMethod_DynamicSecret(t *testing.T) {
	val := &secrets.SecretValue{
		Metadata: secrets.SecretMetadata{IsDynamic: true, LeaseDuration: 30_000_000_000},
	}
	m := pickMethod("agent1", val)
	if m != MethodStdin {
		t.Fatalf("expected stdin for dynamic, got %s", m)
	}
}

func TestPickMethod_SSHKey(t *testing.T) {
	val := &secrets.SecretValue{
		Data: map[string]any{"key": "ssh-private-key"},
	}
	m := pickMethod("agent1", val)
	if m != MethodFile {
		t.Fatalf("expected file for ssh key, got %s", m)
	}
}

func TestPickMethod_NormalSecret(t *testing.T) {
	val := &secrets.SecretValue{
		Data: map[string]any{"key": "api-token"},
	}
	m := pickMethod("agent1", val)
	if m != MethodEnv {
		t.Fatalf("expected env for normal secret, got %s", m)
	}
}

func TestPickMethod_Nil(t *testing.T) {
	m := pickMethod("agent1", nil)
	if m != MethodEnv {
		t.Fatalf("expected env for nil, got %s", m)
	}
}

func TestExtractKey_FromData(t *testing.T) {
	val := &secrets.SecretValue{Data: map[string]any{"key": "my-api-key"}}
	k := extractKey(val, "fallback")
	if k != "my-api-key" {
		t.Fatalf("expected my-api-key, got %s", k)
	}
}

func TestExtractKey_FromPath(t *testing.T) {
	val := &secrets.SecretValue{Data: map[string]any{}}
	k := extractKey(val, "clients/acme/ssh-private-key")
	if k != "SSH_PRIVATE_KEY" {
		t.Fatalf("expected SSH_PRIVATE_KEY, got %s", k)
	}
}

func TestExtractKey_Empty(t *testing.T) {
	val := &secrets.SecretValue{Data: map[string]any{}}
	k := extractKey(val, "")
	if k != "secret" {
		t.Fatalf("expected 'secret', got %s", k)
	}
}

func TestIsFileType(t *testing.T) {
	cases := []struct {
		key  string
		want bool
	}{
		{"ssh-private-key", true},
		{"private_key", true},
		{"tls-cert", true},
		{"certificate", true},
		{"api_token", false},
		{"password", false},
	}
	for _, tc := range cases {
		got := isFileType(tc.key)
		if got != tc.want {
			t.Errorf("isFileType(%q) = %v, want %v", tc.key, got, tc.want)
		}
	}
}

func TestBackendFromURI(t *testing.T) {
	cases := []struct {
		uri  string
		want string
	}{
		{"ref:oap://vault/ws/path", "vault"},
		{"ref:oap://mem/ws/path", "mem"},
		{"ref:oap://x", "x"},
		{"ref:oap://", ""},
		{"short", ""},
	}
	for _, tc := range cases {
		got := backendFromURI(tc.uri)
		if got != tc.want {
			t.Errorf("backendFromURI(%q) = %q, want %q", tc.uri, got, tc.want)
		}
	}
}

func TestSplitLast(t *testing.T) {
	cases := []struct {
		s    string
		sep  byte
		want string
	}{
		{"a/b/c", '/', "c"},
		{"abc", '/', "abc"},
		{"", '/', ""},
	}
	for _, tc := range cases {
		got := splitLast(tc.s, tc.sep)
		if got != tc.want {
			t.Errorf("splitLast(%q, %q) = %q, want %q", tc.s, tc.sep, got, tc.want)
		}
	}
}

func TestSanitizeKey(t *testing.T) {
	got := sanitizeKey("my-api-key.v1")
	if got != "MY_API_KEY_V1" {
		t.Fatalf("expected MY_API_KEY_V1, got %s", got)
	}
}

func TestInjectionSpec_AgentID(t *testing.T) {
	s := InjectionSpec{}
	s.SetAgentID("agent-1")
	if s.AgentID() != "agent-1" {
		t.Fatalf("expected agent-1, got %s", s.AgentID())
	}
}
