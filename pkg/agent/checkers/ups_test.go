package checkers

import (
	"testing"

	"github.com/gosnmp/gosnmp"
)

func TestUPSCheckerImplementsInterface(t *testing.T) {
	var _ Checker = (*UPSChecker)(nil)
	var _ MetaChecker = (*UPSChecker)(nil)
}

func TestUPSCheckerName(t *testing.T) {
	c := &UPSChecker{}
	if c.Name() != "ups_snmp" {
		t.Errorf("Name = %q, want ups_snmp", c.Name())
	}
}

func TestStandardUPSOIDs(t *testing.T) {
	oids := standardUPSOIDs()
	if len(oids) == 0 {
		t.Fatal("standardUPSOIDs returned empty list")
	}
	for _, o := range oids {
		if len(o) < 3 || o[:2] != ".1" {
			t.Errorf("OID %q does not look like an OID", o)
		}
	}
}

func TestSnmpVersion(t *testing.T) {
	if snmpVersion("3") != gosnmp.Version3 {
		t.Error("snmpVersion(3) should be Version3")
	}
	if snmpVersion("2c") != gosnmp.Version2c {
		t.Error("snmpVersion(2c) should be Version2c")
	}
	if snmpVersion("") != gosnmp.Version2c {
		t.Error("snmpVersion(\"\") default should be Version2c")
	}
}

func TestDecodeBatteryStatus(t *testing.T) {
	cases := map[int]string{
		2: "OK",  // battery normal — RFC 1628 upsBatteryStatus
		3: "LB",  // battery low
		4: "RB",  // battery depleted
		1: "unknown",
		7: "unknown",
	}
	for code, want := range cases {
		if got := decodeBatteryStatus(code); got != want {
			t.Errorf("decodeBatteryStatus(%d) = %q, want %q", code, got, want)
		}
	}
}

func TestParseUPSValues(t *testing.T) {
	// upsOutputStatus=2 → onLine (OL); upsBatteryStatus=2 → battery normal (OK)
	vars := []gosnmp.SnmpPDU{
		{Name: ".1.3.6.1.2.1.33.1.1.2.0", Value: []byte("APC Smart-UPS 1500")},
		{Name: ".1.3.6.1.2.1.33.1.2.1.0", Value: 2},  // battery normal
		{Name: ".1.3.6.1.2.1.33.1.4.1.0", Value: 2},   // output on-line
		{Name: ".1.3.6.1.2.1.33.1.4.4.0", Value: 35},
		{Name: ".1.3.6.1.2.1.33.1.4.6.0", Value: 230},
	}
	// First poll: prior state is "". upsOutputStatus=2 hits the
	// "onLine" branch but only fires a transition when prior was OB,
	// so no PowerTransition is set. Status stays "" (not transitioned).
	r := parseUPSValues(vars)
	if r.UPSModel != "APC Smart-UPS 1500" {
		t.Errorf("UPSModel = %q", r.UPSModel)
	}
	if r.BatteryStatus != "OK" {
		t.Errorf("BatteryStatus = %q, want OK (derived from upsBatteryStatus)", r.BatteryStatus)
	}
	if r.LoadPercent != 35 {
		t.Errorf("LoadPercent = %d, want 35", r.LoadPercent)
	}
	// Now the battery goes away. We pass a different upsOutputStatus=3.
	// Status is still "" from before, so the OB branch fires the
	// on_battery transition (any non-OB prior → OB records transition).
	vars2 := []gosnmp.SnmpPDU{
		{Name: ".1.3.6.1.2.1.33.1.4.1.0", Value: 3},
		{Name: ".1.3.6.1.2.1.33.1.2.1.0", Value: 2},
	}
	r2 := parseUPSValues(vars2)
	if r2.UPSStatus != "OB" {
		t.Errorf("UPSStatus = %q, want OB", r2.UPSStatus)
	}
	if r2.PowerTransition != "on_battery" {
		t.Errorf("PowerTransition = %q, want on_battery", r2.PowerTransition)
	}
}

// TestParseUPSValues_OnBattery verifies that upsOutputStatus=3 sets
// UPSStatus to OB and PowerTransition to "on_battery" — this is the
// MIB-correct detection of running on battery (vs. the previous bug
// where upsBatteryStatus was misused as the line state).
func TestParseUPSValues_OnBattery(t *testing.T) {
	vars := []gosnmp.SnmpPDU{
		{Name: ".1.3.6.1.2.1.33.1.4.1.0", Value: 3}, // onBattery
		{Name: ".1.3.6.1.2.1.33.1.2.1.0", Value: 2}, // battery normal (health)
	}
	r := parseUPSValues(vars)
	if r.UPSStatus != "OB" {
		t.Errorf("UPSStatus = %q, want OB", r.UPSStatus)
	}
	if r.PowerTransition != "on_battery" {
		t.Errorf("PowerTransition = %q, want on_battery", r.PowerTransition)
	}
}
