package patches

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/openagentplatform/openagentplatform/pkg/agent/patcher"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// fakeKBStore records the ingest calls made by the consumer.
type fakeKBStore struct {
	scans      []kbScanCall
	installs   []kbInstallCall
	reboots    []kbRebootCall
	scanErr    error
	installErr error
	rebootErr  error
}

type kbScanCall struct {
	orgID, agentID, kb, severity string
}
type kbInstallCall struct {
	orgID, agentID, kb string
	success, reboot    bool
	errMsg             string
}
type kbRebootCall struct {
	orgID, agentID string
	kbs            []string
}

func (f *fakeKBStore) IngestKBScan(ctx context.Context, orgID, agentID, kb, severity string) (string, error) {
	f.scans = append(f.scans, kbScanCall{orgID, agentID, kb, severity})
	return "scanned", f.scanErr
}
func (f *fakeKBStore) IngestKBInstall(ctx context.Context, orgID, agentID, kb string, success, reboot bool, errMsg string) (string, error) {
	f.installs = append(f.installs, kbInstallCall{orgID, agentID, kb, success, reboot, errMsg})
	return "installed", f.installErr
}
func (f *fakeKBStore) IngestKBRebootDone(ctx context.Context, orgID, agentID string, kbs []string) error {
	f.reboots = append(f.reboots, kbRebootCall{orgID, agentID, kbs})
	return f.rebootErr
}

// fakeResolver returns a fixed org for any agent id.
type fakeResolver struct {
	orgID string
}

func (r *fakeResolver) GetAgent(ctx context.Context, orgID, id string) (*models.Agent, error) {
	return &models.Agent{ID: id, OrgID: r.orgID}, nil
}

// TestKBConsumer_ScanRoundTrip verifies that a published PatchKBScanEnvelope
// is decoded and fed to IngestKBScan with kb derived from KBID (fallback
// Name) and severity from the patch.
func TestKBConsumer_ScanRoundTrip(t *testing.T) {
	store := &fakeKBStore{}
	resolver := &fakeResolver{orgID: "org-999"}
	c := &KBConsumer{store: store, resolver: resolver}

	env := patcher.PatchKBScanEnvelope{
		AgentID:    "agent-1",
		Patches:    []patcher.PatchInfo{{KBID: "KB5001234", Name: "CVE-2024-x", Severity: "critical"}},
		ReceivedAt: time.Now(),
	}
	payload, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	c.handleScan(&nats.Msg{Data: payload})

	if len(store.scans) != 1 {
		t.Fatalf("expected 1 scan, got %d", len(store.scans))
	}
	s := store.scans[0]
	if s.orgID != "org-999" || s.agentID != "agent-1" || s.kb != "KB5001234" || s.severity != "critical" {
		t.Errorf("unexpected scan call %+v", s)
	}
}

// TestKBConsumer_ScanFallbackToName verifies that when KBID is empty the
// consumer falls back to Name as the per-KB key.
func TestKBConsumer_ScanFallbackToName(t *testing.T) {
	store := &fakeKBStore{}
	resolver := &fakeResolver{orgID: "org-999"}
	c := &KBConsumer{store: store, resolver: resolver}

	env := patcher.PatchKBScanEnvelope{
		AgentID: "agent-1",
		Patches: []patcher.PatchInfo{{Name: "KB-NAME-ONLY", Severity: "important"}},
	}
	payload, _ := json.Marshal(env)
	c.handleScan(&nats.Msg{Data: payload})

	if len(store.scans) != 1 || store.scans[0].kb != "KB-NAME-ONLY" {
		t.Fatalf("expected kb fallback to name, got %+v", store.scans)
	}
}

// TestKBConsumer_ScanSkipsEmpty verifies that rows with both KBID and Name
// empty are skipped.
func TestKBConsumer_ScanSkipsEmpty(t *testing.T) {
	store := &fakeKBStore{}
	resolver := &fakeResolver{orgID: "org-999"}
	c := &KBConsumer{store: store, resolver: resolver}

	env := patcher.PatchKBScanEnvelope{
		AgentID: "agent-1",
		Patches: []patcher.PatchInfo{{Severity: "low"}}, // no kb, no name
	}
	payload, _ := json.Marshal(env)
	c.handleScan(&nats.Msg{Data: payload})

	if len(store.scans) != 0 {
		t.Fatalf("expected 0 scans for empty kb/name, got %d", len(store.scans))
	}
}

// TestKBConsumer_InstallRoundTrip verifies a PatchKBInstallEnvelope with a
// successful, reboot-required result feeds IngestKBInstall correctly.
func TestKBConsumer_InstallRoundTrip(t *testing.T) {
	store := &fakeKBStore{}
	resolver := &fakeResolver{orgID: "org-999"}
	c := &KBConsumer{store: store, resolver: resolver}

	env := patcher.PatchKBInstallEnvelope{
		AgentID: "agent-1",
		Patch:   &patcher.PatchInfo{KBID: "KB5001234"},
		Result:  &patcher.InstallResult{Success: true, RebootRequired: true},
	}
	payload, _ := json.Marshal(env)
	c.handleInstall(&nats.Msg{Data: payload})

	if len(store.installs) != 1 {
		t.Fatalf("expected 1 install, got %d", len(store.installs))
	}
	in := store.installs[0]
	if in.kb != "KB5001234" || !in.success || !in.reboot {
		t.Errorf("unexpected install call %+v", in)
	}
}

// TestKBConsumer_InstallFailureMsg verifies that a failed install carries
// the error message into IngestKBInstall.
func TestKBConsumer_InstallFailureMsg(t *testing.T) {
	store := &fakeKBStore{}
	resolver := &fakeResolver{orgID: "org-999"}
	c := &KBConsumer{store: store, resolver: resolver}

	env := patcher.PatchKBInstallEnvelope{
		AgentID: "agent-1",
		Patch:   &patcher.PatchInfo{KBID: "KB5001234"},
		Result:  &patcher.InstallResult{Success: false, ErrorMessage: "0x80070005"},
	}
	payload, _ := json.Marshal(env)
	c.handleInstall(&nats.Msg{Data: payload})

	if len(store.installs) != 1 {
		t.Fatalf("expected 1 install, got %d", len(store.installs))
	}
	if store.installs[0].success || store.installs[0].errMsg != "0x80070005" {
		t.Errorf("unexpected install call %+v", store.installs[0])
	}
}

// TestKBConsumer_RebootDoneRoundTrip verifies the reboot envelope feeds
// IngestKBRebootDone with the listed KBs.
func TestKBConsumer_RebootDoneRoundTrip(t *testing.T) {
	store := &fakeKBStore{}
	resolver := &fakeResolver{orgID: "org-999"}
	c := &KBConsumer{store: store, resolver: resolver}

	env := patcher.PatchKBRebootEnvelope{
		AgentID:    "agent-1",
		KBs:        []string{"KB5001234", "KB5001235"},
		ReceivedAt: time.Now(),
	}
	payload, _ := json.Marshal(env)
	c.handleRebootDone(&nats.Msg{Data: payload})

	if len(store.reboots) != 1 {
		t.Fatalf("expected 1 reboot, got %d", len(store.reboots))
	}
	r := store.reboots[0]
	if r.orgID != "org-999" || r.agentID != "agent-1" || len(r.kbs) != 2 {
		t.Errorf("unexpected reboot call %+v", r)
	}
}
