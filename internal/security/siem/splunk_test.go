package siem

import (
	"testing"

	"github.com/openagentplatform/openagentplatform/internal/security"
)

func TestSplunkForwarderImplementsInterface(t *testing.T) {
	f := NewSplunkForwarder("https://splunk:8088/services/collector", "token")
	var _ security.SIEMForwarder = f
}

func TestElasticForwarderImplementsInterface(t *testing.T) {
	f := NewElasticForwarder("https://elastic:9200/oap-security/_bulk", "key")
	var _ security.SIEMForwarder = f
}

func TestGenericForwarderImplementsInterface(t *testing.T) {
	f := NewGenericForwarder("https://siem.example.com/ingest", "cef", "token")
	var _ security.SIEMForwarder = f
}

func TestSeverityToInt(t *testing.T) {
	cases := map[string]int{
		"critical": 10,
		"warning":  5,
		"info":     1,
		"":         1,
	}
	for s, want := range cases {
		if got := severityToInt(s); got != want {
			t.Errorf("severityToInt(%q) = %d, want %d", s, got, want)
		}
	}
}

func TestSplunkForwarderName(t *testing.T) {
	if got := string(NewSplunkForwarder("e", "t").Name()); got != "splunk" {
		t.Errorf("Name = %q, want splunk", got)
	}
}

func TestElasticForwarderName(t *testing.T) {
	if got := string(NewElasticForwarder("e", "k").Name()); got != "elastic" {
		t.Errorf("Name = %q, want elastic", got)
	}
}

func TestGenericForwarderName(t *testing.T) {
	if got := string(NewGenericForwarder("e", "cef", "t").Name()); got != "generic" {
		t.Errorf("Name = %q, want generic", got)
	}
}

func TestBatchSize(t *testing.T) {
	if NewSplunkForwarder("e", "t").BatchSize() != 100 {
		t.Error("Splunk BatchSize != 100")
	}
	if NewElasticForwarder("e", "k").BatchSize() != 100 {
		t.Error("Elastic BatchSize != 100")
	}
	if NewGenericForwarder("e", "json", "t").BatchSize() != 100 {
		t.Error("Generic BatchSize != 100")
	}
}
