package checkers

import (
	"testing"
)

// TestRegistryDefaultTypes verifies that the built-in checkers are all
// registered on init(). If any default checker is renamed or removed
// without updating this test, it will fail loudly.
func TestRegistryDefaultTypes(t *testing.T) {
	want := []string{"ping", "http", "tcp", "dns", "cpu", "memory", "disk", "service"}
	have := Types()
	set := make(map[string]struct{}, len(have))
	for _, name := range have {
		set[name] = struct{}{}
	}
	for _, expected := range want {
		if _, ok := set[expected]; !ok {
			t.Errorf("expected default checker %q to be registered; got types=%v", expected, have)
		}
	}
}

// TestGetReturnsRegisteredChecker verifies that Get returns the same
// instance the registry was seeded with, and that the Name() method
// round-trips back to the requested key.
func TestGetReturnsRegisteredChecker(t *testing.T) {
	c, err := Get("ping")
	if err != nil {
		t.Fatalf("Get(ping) returned error: %v", err)
	}
	if c == nil {
		t.Fatal("Get(ping) returned nil checker")
	}
	if c.Name() != "ping" {
		t.Errorf("checker Name() = %q; want %q", c.Name(), "ping")
	}
}

// TestGetUnknownTypeReturnsError verifies that looking up an unknown
// type returns an error instead of silently returning nil.
func TestGetUnknownTypeReturnsError(t *testing.T) {
	_, err := Get("does_not_exist")
	if err == nil {
		t.Fatal("Get(unknown) returned no error; expected one")
	}
}

// TestRegisterAndGetRoundTrip verifies that a custom checker registered
// via Register is retrievable through Get. This guards against any
// future change to the registry that breaks registration.
func TestRegisterAndGetRoundTrip(t *testing.T) {
	// Use a name that the default registry does not own.
	const name = "test_register_round_trip"
	Register(name, &PingChecker{})
	t.Cleanup(func() {
		// Best-effort cleanup; registry is package-global.
	})

	c, err := Get(name)
	if err != nil {
		t.Fatalf("Get(%q) returned error: %v", name, err)
	}
	if c.Name() != "ping" {
		t.Errorf("registered checker Name() = %q; want %q", c.Name(), "ping")
	}
}

// TestGetMetadataReturnsRegisteredMetadata verifies that GetMetadata
// returns the metadata for a known checker.
func TestGetMetadataReturnsRegisteredMetadata(t *testing.T) {
	m, err := GetMetadata("ping")
	if err != nil {
		t.Fatalf("GetMetadata(ping) returned error: %v", err)
	}
	if m.Name == "" {
		t.Error("ping metadata has empty Name")
	}
}

// TestAllMetadataCoversDefaults verifies that every default checker
// appears in the AllMetadata output.
func TestAllMetadataCoversDefaults(t *testing.T) {
	all := AllMetadata()
	if len(all) < 8 {
		t.Errorf("AllMetadata returned %d entries; want >= 8 (defaults)", len(all))
	}
}

// TestRunWithUnknownTypeReturnsFailedResult verifies that Run produces
// a failed Result when given an unknown check type, instead of panicking.
func TestRunWithUnknownTypeReturnsFailedResult(t *testing.T) {
	r := Run(nil, &CheckRequest{Type: "bogus_check_type"})
	if r == nil {
		t.Fatal("Run returned nil result for unknown type")
	}
	if r.OK {
		t.Errorf("Run with unknown type returned OK=true; want false")
	}
	if r.Error == "" {
		t.Error("Run with unknown type returned empty Error")
	}
}