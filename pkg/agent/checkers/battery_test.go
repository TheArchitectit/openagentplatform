package checkers

import "testing"

func TestBatteryCheckerName(t *testing.T) {
	c := NewBatteryChecker("linux")
	if c.Name() != "battery" {
		t.Errorf("Name = %q, want battery", c.Name())
	}
}

func TestBatteryCheckerImplementsInterface(t *testing.T) {
	c := NewBatteryChecker("linux")
	var _ Checker = c
	var _ MetaChecker = c
}

func TestBatteryCheckerDefaultPlatform(t *testing.T) {
	c := NewBatteryChecker("")
	if c.platform == "" {
		t.Error("expected default platform to be set")
	}
}
