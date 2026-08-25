package mesh

import (
	"encoding/base64"
	"strings"
	"testing"
)

func testB64Key(bytes int) string {
	raw := make([]byte, bytes)
	return base64.StdEncoding.EncodeToString(raw)
}

func validConfig() *MeshConfig {
	return &MeshConfig{
		AgentID:    "agent-1",
		OrgID:      "org-42",
		PrivateKey: testB64Key(32),
		Address:    "10.0.0.2/32",
		ListenPort: 51820,
	}
}

func TestMeshConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*MeshConfig)
		wantErr string
	}{
		{"valid", func(c *MeshConfig) {}, ""},
		{"missing agent_id", func(c *MeshConfig) { c.AgentID = "" }, "agent_id"},
		{"missing org_id", func(c *MeshConfig) { c.OrgID = "" }, "org_id"},
		{"bad private key base64", func(c *MeshConfig) { c.PrivateKey = "not-b64!!" }, "private_key"},
		{"short private key", func(c *MeshConfig) { c.PrivateKey = testB64Key(16) }, "private_key"},
		{"missing address", func(c *MeshConfig) { c.Address = "" }, "address"},
		{"bad address", func(c *MeshConfig) { c.Address = "not-an-ip" }, "invalid address"},
		{"bad listen port", func(c *MeshConfig) { c.ListenPort = 70000 }, "listen_port"},
		{"bad peer key", func(c *MeshConfig) {
			c.PeerPublicKey = "AAAA" // invalid base64
		}, "peer_public_key"},
		{"bad endpoint", func(c *MeshConfig) {
			c.PeerPublicKey = testB64Key(32)
			c.PeerEndpoint = "no-port"
		}, "peer_endpoint"},
		{"bad peer allowed ip", func(c *MeshConfig) {
			c.PeerAllowedIPs = []string{"not-a-cidr"}
		}, "allowed_ip"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := validConfig()
			tc.mutate(c)
			err := c.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("Validate() expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("Validate() error %q does not contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestParseMeshConfig(t *testing.T) {
	raw := []byte(`{"agent_id":"agent-1","org_id":"org-42","private_key":"` + testB64Key(32) + `","address":"10.0.0.2/32","listen_port":51820}`)
	c, err := ParseMeshConfig(raw)
	if err != nil {
		t.Fatalf("ParseMeshConfig(%s) unexpected error: %v", raw, err)
	}
	if c.AgentID != "agent-1" || c.OrgID != "org-42" || c.Address != "10.0.0.2/32" {
		t.Fatalf("ParseMeshConfig parsed wrong fields: %+v", c)
	}

	if _, err := ParseMeshConfig(nil); err == nil {
		t.Fatal("ParseMeshConfig(nil) expected error")
	}
	bad := []byte(`{"agent_id":"agent-1","org_id":"org-42","private_key":"x","address":"10.0.0.2/32"}`)
	if _, err := ParseMeshConfig(bad); err == nil {
		t.Fatal("ParseMeshConfig(bad key) expected error")
	}
}

func TestMeshConfigUAPIConfig(t *testing.T) {
	c := validConfig()
	c.PeerPublicKey = testB64Key(32)
	c.PeerEndpoint = "192.0.2.10:51820"
	c.PeerAllowedIPs = []string{"10.0.0.0/24"}
	c.PersistentKeepalive = 25

	u := c.uapiConfig()
	for _, want := range []string{
		"private_key=" + c.PrivateKey,
		"listen_port=51820",
		"replace_peers=true",
		"public_key=" + c.PeerPublicKey,
		"endpoint=192.0.2.10:51820",
		"allowed_ip=10.0.0.0/24",
		"persistent_keepalive_interval=25",
	} {
		if !strings.Contains(u, want) {
			t.Fatalf("uapiConfig() missing %q in:\n%s", want, u)
		}
	}

	// No peer -> no replace_peers, no allowed_ip, key-only stanza.
	noPeer := validConfig()
	if u := noPeer.uapiConfig(); strings.Contains(u, "replace_peers") {
		t.Fatalf("uapiConfig() with no peer should not emit replace_peers, got:\n%s", u)
	}
}

func TestMeshConfigDefaults(t *testing.T) {
	c := validConfig()
	if got := c.interfaceName(); got != DefaultMeshInterface {
		t.Fatalf("interfaceName() = %q, want %q", got, DefaultMeshInterface)
	}
	if got := c.mtu(); got != defaultMeshMTU {
		t.Fatalf("mtu() = %d, want %d", got, defaultMeshMTU)
	}
	if got := c.listenPort(); got != defaultMeshPort {
		t.Fatalf("listenPort() = %d, want %d", got, defaultMeshPort)
	}

	c2 := validConfig()
	c2.Interface = "custom0"
	c2.MTU = 1400
	c2.ListenPort = 51999
	if got := c2.interfaceName(); got != "custom0" {
		t.Fatalf("interfaceName() override = %q", got)
	}
	if got := c2.mtu(); got != 1400 {
		t.Fatalf("mtu() override = %d", got)
	}
	if got := c2.listenPort(); got != 51999 {
		t.Fatalf("listenPort() override = %d", got)
	}
}
