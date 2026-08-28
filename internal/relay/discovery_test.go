package relay

import (
	"testing"
	"time"

	"github.com/openagentplatform/openagentplatform/a2a/models"
)

func testEnvelope(id, tenant string, version uint64, scope VisibilityScope, skills []models.Skill, ttl time.Duration) *DiscoveryEnvelope {
	return &DiscoveryEnvelope{
		Record: models.AgentCard{ID: id, Skills: skills},
		Provenance: Provenance{TenantID: tenant, PublishedAt: time.Now()},
		Visibility: Visibility{Scope: scope},
		TTL:        ttl,
		Version:    version,
	}
}

func TestDiscoveryRegistry_PublishValid(t *testing.T) {
	d := NewDiscoveryRegistry("relay-1", nil)
	env := testEnvelope("agent-a", "t1", 1, VisibilityTenantPrivate, nil, time.Hour)
	if err := d.Publish(env); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if len(d.Snapshot()) != 1 {
		t.Fatalf("expected 1 record, got %d", len(d.Snapshot()))
	}
}

func TestDiscoveryRegistry_PublishVersionReplay(t *testing.T) {
	d := NewDiscoveryRegistry("relay-1", nil)
	d.Publish(testEnvelope("agent-a", "t1", 5, VisibilityTenantPrivate, nil, time.Hour))
	if err := d.Publish(testEnvelope("agent-a", "t1", 5, VisibilityTenantPrivate, nil, time.Hour)); err == nil {
		t.Fatal("expected version replay rejection")
	}
	if err := d.Publish(testEnvelope("agent-a", "t1", 4, VisibilityTenantPrivate, nil, time.Hour)); err == nil {
		t.Fatal("expected lower-version rejection")
	}
}

func TestDiscoveryRegistry_PublishTTLCap(t *testing.T) {
	d := NewDiscoveryRegistry("relay-1", nil)
	if err := d.Publish(testEnvelope("agent-a", "t1", 1, VisibilityTenantPrivate, nil, 25*time.Hour)); err == nil {
		t.Fatal("expected over-long TTL rejection")
	}
}

func TestDiscoveryRegistry_WithdrawAndTombstone(t *testing.T) {
	d := NewDiscoveryRegistry("relay-1", nil)
	d.Publish(testEnvelope("agent-a", "t1", 1, VisibilityTenantPrivate, nil, time.Hour))

	if err := d.Withdraw("agent-a", "t1", 2); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	if _, ok := d.Snapshot()["agent-a"]; ok {
		t.Fatal("record should be gone after withdraw")
	}
	if err := d.Publish(testEnvelope("agent-a", "t1", 2, VisibilityTenantPrivate, nil, time.Hour)); err == nil {
		t.Fatal("expected tombstone rejection at withdraw version")
	}
	if err := d.Publish(testEnvelope("agent-a", "t1", 3, VisibilityTenantPrivate, nil, time.Hour)); err != nil {
		t.Fatalf("expected publish above tombstone to succeed: %v", err)
	}
}

func TestDiscoveryRegistry_WithdrawTenantOwner(t *testing.T) {
	d := NewDiscoveryRegistry("relay-1", nil)
	d.Publish(testEnvelope("agent-a", "t1", 1, VisibilityTenantPrivate, nil, time.Hour))
	if err := d.Withdraw("agent-a", "t2", 2); err == nil {
		t.Fatal("expected tenant ownership rejection")
	}
}

func TestDiscoveryRegistry_ResolveScopes(t *testing.T) {
	d := NewDiscoveryRegistry("relay-1", nil)
	private := testEnvelope("pvt", "t1", 1, VisibilityTenantPrivate, nil, time.Hour)
	allow := testEnvelope("alw", "t1", 1, VisibilityTenantAllowlisted, nil, time.Hour)
	allow.Visibility.Allowlist = []string{"t2"}
	public := testEnvelope("pub", "t3", 1, VisibilityGlobalPublic, nil, time.Hour)

	for _, e := range []*DiscoveryEnvelope{private, allow, public} {
		if err := d.Publish(e); err != nil {
			t.Fatal(err)
		}
	}

	// t1 (owner) sees all three.
	got := d.Resolve("t1", "")
	if len(got) != 3 {
		t.Fatalf("owner should see 3 records, got %d", len(got))
	}

	// t2 is allowlisted on "alw" and sees pub (global).
	got = d.Resolve("t2", "")
	if len(got) != 2 {
		t.Fatalf("t2 should see 2 records (allowlisted + global), got %d", len(got))
	}
	for _, e := range got {
		if e.Record.ID == "pvt" {
			t.Fatal("t2 must not see private record")
		}
	}

	// t4 sees only global.
	got = d.Resolve("t4", "")
	if len(got) != 1 || got[0].Record.ID != "pub" {
		t.Fatalf("t4 should see only public, got %d", len(got))
	}
}

func TestDiscoveryRegistry_OperatorAllowlists(t *testing.T) {
	d := NewDiscoveryRegistry("relay-1", nil)
	d.SetOperatorAllowlists(map[string][]string{
		"t1": {"t2"}, // t1 allows t2 broadly
	})

	allow := testEnvelope("alw", "t1", 1, VisibilityTenantAllowlisted, nil, time.Hour)
	// No per-record allowlist — operator allowlist should still grant t2.
	if err := d.Publish(allow); err != nil {
		t.Fatal(err)
	}
	got := d.Resolve("t2", "")
	if len(got) != 1 {
		t.Fatalf("t2 should see t1's record via operator allowlist, got %d", len(got))
	}

	private := testEnvelope("pvt", "t1", 1, VisibilityTenantPrivate, nil, time.Hour)
	d.Publish(private)
	got = d.Resolve("t2", "")
	for _, e := range got {
		if e.Record.ID == "pvt" {
			t.Fatal("operator allowlist must not override tenant_private")
		}
	}
}

func TestDiscoveryRegistry_ResolveSkillFilterAndSort(t *testing.T) {
	d := NewDiscoveryRegistry("relay-1", nil)
	a := testEnvelope("a", "t1", 1, VisibilityTenantPrivate, []models.Skill{
		{ID: "triage", Name: "Triage"},
	}, time.Hour)
	b := testEnvelope("b", "t1", 2, VisibilityTenantPrivate, []models.Skill{
		{ID: "patch", Name: "Patch"},
	}, time.Hour)
	c := testEnvelope("c", "t1", 3, VisibilityTenantPrivate, nil, time.Hour)
	for _, e := range []*DiscoveryEnvelope{a, b, c} {
		d.Publish(e)
	}

	got := d.Resolve("t1", "triage")
	if len(got) != 1 || got[0].Record.ID != "a" {
		t.Fatalf("triage filter should return only agent-a, got %d", len(got))
	}
}

func TestDiscoveryRegistry_Expire(t *testing.T) {
	d := NewDiscoveryRegistry("relay-1", nil)
	env := testEnvelope("a", "t1", 1, VisibilityTenantPrivate, nil, time.Hour)
	env.Provenance.PublishedAt = time.Now().Add(-2 * time.Hour)
	d.Publish(env)

	if n := d.Expire(time.Now()); n != 1 {
		t.Fatalf("expected 1 expiry, got %d", n)
	}
	if len(d.Snapshot()) != 0 {
		t.Fatal("record should be gone after expiry")
	}
}
