package patches

import (
	"errors"
	"testing"
)

// TestWinUpdateNextState_Legal verifies every legal transition in the
// RMM-03 WinUpdate state machine advances to the expected state.
func TestWinUpdateNextState_Legal(t *testing.T) {
	cases := []struct {
		from   string
		event  string
		expect string
	}{
		{"scanned", "approve", "approved"},
		{"scanned", "queue", "pending_approval"},
		{"scanned", "reject", "rejected"},
		{"pending_approval", "approve", "approved"},
		{"pending_approval", "reject", "rejected"},
		{"approved", "install", "installing"},
		{"approved", "reject", "rejected"},
		{"rejected", "rescan", "scanned"},
		{"installing", "complete", "installed"},
		{"installing", "fail", "failed"},
		{"installing", "reboot", "reboot_required"},
		{"failed", "install", "installing"},
		{"reboot_required", "reboot_done", "installed"},
	}
	for _, c := range cases {
		got, err := WinUpdateNextState(c.from, c.event)
		if err != nil {
			t.Errorf("WinUpdateNextState(%q,%q): unexpected error %v", c.from, c.event, err)
			continue
		}
		if got != c.expect {
			t.Errorf("WinUpdateNextState(%q,%q): got %q, want %q", c.from, c.event, got, c.expect)
		}
	}
}

// TestWinUpdateNextState_Terminal verifies that installed is terminal and
// that illegal transitions return ErrInvalidTransition.
func TestWinUpdateNextState_Terminal(t *testing.T) {
	// installed is terminal: no outgoing transitions.
	if _, err := WinUpdateNextState("installed", "anything"); err == nil {
		t.Error("installed->anything should be illegal")
	}
}

// TestWinUpdateNextState_Illegal verifies specific illegal transitions
// return ErrInvalidTransition (not a different sentinel).
func TestWinUpdateNextState_Illegal(t *testing.T) {
	cases := []struct {
		from  string
		event string
	}{
		{"installed", "approve"},        // terminal state
		{"reboot_required", "complete"}, // wrong event from reboot_required
		{"scanned", "complete"},         // no direct scan->installed
		{"approved", "approve"},         // already approved
		{"rejected", "approve"},         // rejected is terminal-ish
		{"pending_approval", "install"}, // must approve first
		{"installing", "approve"},       // installing is not a user state
		{"failed", "complete"},          // failed must go through install
		{"", "approve"},                 // unknown state
		{"scanned", ""},                 // unknown event
		{"scanned", "reboot_done"},      // event only valid from reboot_required
		{"bogus_state", "queue"},        // unknown state
	}
	for _, c := range cases {
		got, err := WinUpdateNextState(c.from, c.event)
		if !errors.Is(err, ErrInvalidTransition) {
			t.Errorf("WinUpdateNextState(%q,%q): expected ErrInvalidTransition, got state=%q err=%v",
				c.from, c.event, got, err)
		}
	}
}

// TestWinUpdateStateConstants verifies the eight-state vocabulary is the
// canonical one reused from the patch_job_targets CHECK constraint.
func TestWinUpdateStateConstants(t *testing.T) {
	want := []string{
		"scanned", "pending_approval", "approved", "rejected",
		"installing", "installed", "failed", "reboot_required",
	}
	got := []string{
		WinUpdateStateScanned, WinUpdateStatePendingApproval, WinUpdateStateApproved,
		WinUpdateStateRejected, WinUpdateStateInstalling, WinUpdateStateInstalled,
		WinUpdateStateFailed, WinUpdateStateRebootRequired,
	}
	if len(got) != len(want) {
		t.Fatalf("state count: got %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("state[%d]: got %q, want %q", i, got[i], want[i])
		}
	}
}

// TestWinUpdateValidTransitions_InstalledTerminal confirms installed has no
// outgoing events in the map (it is not even present as a key).
func TestWinUpdateValidTransitions_InstalledTerminal(t *testing.T) {
	if _, ok := WinUpdateValidTransitions[WinUpdateStateInstalled]; ok {
		t.Error("installed must not appear as a key in WinUpdateValidTransitions")
	}
}
