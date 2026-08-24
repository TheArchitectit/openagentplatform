package patches

// WinUpdate state machine: the per-KB Windows Update lifecycle.
//
// The vocabulary is the canonical eight-state set shared with the
// patch_job_targets CHECK constraint (ck_patch_job_targets_state). We
// reuse these exact strings so the winupdate_kb_state table and the
// patch_job_targets table share one state vocabulary.
//
// This machine is intentionally independent of the ApprovalWorkflow
// used for patch jobs: it tracks individual KB articles reported by the
// agent, not deployment jobs. Transitions are driven by agent reports
// (scan, install result, reboot done) and by the auto-approve policy
// (critical severity auto-approves on first scan).

const (
	WinUpdateStateScanned         = "scanned"
	WinUpdateStatePendingApproval = "pending_approval"
	WinUpdateStateApproved        = "approved"
	WinUpdateStateRejected        = "rejected"
	WinUpdateStateInstalling      = "installing"
	WinUpdateStateInstalled       = "installed"
	WinUpdateStateFailed          = "failed"
	WinUpdateStateRebootRequired  = "reboot_required"
)

// WinUpdate events.
const (
	WinUpdateEventApprove    = "approve"     // scanned -> approved
	WinUpdateEventQueue      = "queue"       // scanned -> pending_approval
	WinUpdateEventReject     = "reject"      // scanned/approved -> rejected
	WinUpdateEventRescan     = "rescan"      // rejected -> scanned
	WinUpdateEventInstall    = "install"     // approved -> installing (and failed -> installing)
	WinUpdateEventComplete   = "complete"    // installing -> installed
	WinUpdateEventFail       = "fail"        // installing -> failed
	WinUpdateEventReboot     = "reboot"      // installing -> reboot_required
	WinUpdateEventRebootDone = "reboot_done" // reboot_required -> installed
)

// WinUpdateValidTransitions defines the legal (state, event) -> nextState
// mappings for the per-KB WinUpdate lifecycle. Any (state, event) pair
// not present is rejected by WinUpdateNextState.
var WinUpdateValidTransitions = map[string]map[string]string{
	WinUpdateStateScanned: {
		WinUpdateEventApprove: WinUpdateStateApproved,
		WinUpdateEventQueue:   WinUpdateStatePendingApproval,
		WinUpdateEventReject:  WinUpdateStateRejected,
	},
	WinUpdateStatePendingApproval: {
		WinUpdateEventApprove: WinUpdateStateApproved,
		WinUpdateEventReject:  WinUpdateStateRejected,
	},
	WinUpdateStateApproved: {
		WinUpdateEventInstall: WinUpdateStateInstalling,
		WinUpdateEventReject:  WinUpdateStateRejected,
	},
	WinUpdateStateRejected: {
		WinUpdateEventRescan: WinUpdateStateScanned,
	},
	WinUpdateStateInstalling: {
		WinUpdateEventComplete: WinUpdateStateInstalled,
		WinUpdateEventFail:     WinUpdateStateFailed,
		WinUpdateEventReboot:   WinUpdateStateRebootRequired,
	},
	WinUpdateStateFailed: {
		WinUpdateEventInstall: WinUpdateStateInstalling,
	},
	WinUpdateStateRebootRequired: {
		WinUpdateEventRebootDone: WinUpdateStateInstalled,
	},
	// WinUpdateStateInstalled is terminal: no outgoing transitions.
}

// WinUpdateNextState returns the destination state for a (state, event)
// pair using the per-KB WinUpdate machine. It returns ErrInvalidTransition
// (the same sentinel as the patch-job workflow) for unknown states or
// events.
func WinUpdateNextState(state, event string) (string, error) {
	events, ok := WinUpdateValidTransitions[state]
	if !ok {
		return "", &winUpdateInvalidTransitionError{state: state, event: event}
	}
	next, ok := events[event]
	if !ok {
		return "", &winUpdateInvalidTransitionError{state: state, event: event}
	}
	return next, nil
}

// winUpdateInvalidTransitionError wraps ErrInvalidTransition so callers
// can detect it with errors.Is, reusing the established sentinel.
type winUpdateInvalidTransitionError struct {
	state string
	event string
}

func (e *winUpdateInvalidTransitionError) Error() string {
	return "invalid winupdate state transition: state " + e.state + " event " + e.event
}

func (e *winUpdateInvalidTransitionError) Unwrap() error {
	return ErrInvalidTransition
}
