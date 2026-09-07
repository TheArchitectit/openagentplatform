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
		2: "OL",
		3: "LB",
		4: "RB",
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
	vars := []gosnmp.SnmpPDU{
		{Name: ".1.3.6.1.2.1.33.1.1.2.0", Value: []byte("APC Smart-UPS 1500")},
		{Name: ".1.3.6.1.2.1.33.1.2.1.0", Value: 2},
		{Name: ".1.3.6.1.2.1.33.1.4.4.0", Value: 35},
		{Name: ".1.3.6.1.2.1.33.1.4.6.0", Value: 230},
	}
	r := parseUPSValues(vars)
	if r.UPSModel != "APC Smart-UPS 1500" {
		t.Errorf("UPSModel = %q", r.UPSModel)
	}
	if r.UPSStatus != "OL" {
		t.Errorf("UPSStatus = %q, want OL", r.UPSStatus)
	}
	if r.LoadPercent != 35 {
		t.Errorf("LoadPercent = %d, want 35", r.LoadPercent)
	}
}
