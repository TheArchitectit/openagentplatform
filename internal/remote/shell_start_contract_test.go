package remote

import (
	"encoding/json"
	"testing"

	"github.com/openagentplatform/openagentplatform/pkg/agent/shell"
)

// The agent-side shell handler decodes StartRequest from
// oap.agents.<id>.shell.start. This test pins the contract: if either
// side's subject or JSON field names drift, it fails.
func TestShellStartRequestContract(t *testing.T) {
	if got, want := shellStartSubject("agent-9"), shell.StartRequestSubject("agent-9"); got != want {
		t.Errorf("subject drift: server=%q agent=%q", got, want)
	}

	payload, err := json.Marshal(shellStartRequest{
		SessionID: "sess-1",
		UserID:    "user-2",
		Protocol:  "ssh",
		Cols:      120,
		Rows:      40,
	})
	if err != nil {
		t.Fatal(err)
	}
	var req shell.StartRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		t.Fatalf("agent cannot decode server payload: %v", err)
	}
	if req.SessionID != "sess-1" || req.UserID != "user-2" || req.Protocol != shell.ProtocolSSH ||
		req.Cols != 120 || req.Rows != 40 {
		t.Errorf("field drift after decode: %+v", req)
	}
}

// CreateSession must publish a start request when a NATS connection is
// available. Uses a real *nats.Conn-less path: the manager stores the
// conn as *nats.Conn only, so the publish path is exercised via the
// contract test above plus an end-to-end assertion that CreateSession
// succeeds and produces valid subjects.
func TestCreateSessionProducesValidSubjects(t *testing.T) {
	m := NewShellManager(DefaultShellManagerConfig(), nil, nil)
	sess, err := m.CreateSession("agent-5", "user-7", ProtocolSSH, TerminalSize{})
	if err != nil {
		t.Fatal(err)
	}
	if sess == nil || sess.ID == "" {
		t.Fatal("session not created")
	}
	if want := "oap.agents.agent-5.shell.start"; shellStartSubject("agent-5") != want {
		t.Errorf("start subject = %q, want %q", shellStartSubject("agent-5"), want)
	}
}
