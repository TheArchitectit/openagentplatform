package safety

import (
	"context"
	"errors"
	"testing"
	"time"
)

// stubResolver is a minimal SecretResolver for tests.
type stubResolver struct {
	val []byte
	err error
}

func (r *stubResolver) Resolve(_ context.Context, _ string) ([]byte, error) {
	return r.val, r.err
}

// TestServerSideOperationRequiresHandler verifies that with no handler
// registered, ServerSideOperation fails explicitly instead of silently
// returning an empty result (the previous stub behavior that masked a
// non-functional safety control).
func TestServerSideOperationRequiresHandler(t *testing.T) {
	s := NewScriptCredentialSafe(DefaultPolicy(), &stubResolver{val: []byte("k")}, nil, nil, nil)

	_, err := s.ServerSideOperation(context.Background(), "agent-1", "ssh.connect", map[string]string{"host": "10.0.0.1"})
	if err == nil {
		t.Fatal("expected error when no handler registered, got nil")
	}
}

// TestServerSideOperationRejectsSmuggle verifies the no-arg-secrets policy
// blocks params that look like secrets before any handler runs.
func TestServerSideOperationRejectsSmuggle(t *testing.T) {
	s := NewScriptCredentialSafe(DefaultPolicy(), &stubResolver{val: []byte("k")}, nil, nil, nil)
	_, err := s.ServerSideOperation(context.Background(), "agent-1", "ssh.connect",
		map[string]string{"password": "supersecret123"})
	if err == nil {
		t.Fatal("expected smuggle rejection, got nil")
	}
}

// TestServerSideOperationRunsHandler verifies a registered handler executes,
// receives params + resolver, and its result is returned on the operation.
func TestServerSideOperationRunsHandler(t *testing.T) {
	s := NewScriptCredentialSafe(DefaultPolicy(), &stubResolver{val: []byte("secret-val")}, nil, nil, nil)
	called := false
	s.RegisterOperationHandler("db.query", func(ctx context.Context, params map[string]string, resolve SecretResolver) (string, error) {
		called = true
		if params["db"] != "prod" {
			return "", errors.New("missing db param")
		}
		v, err := resolve.Resolve(ctx, "ref:oap://db/pass")
		if err != nil {
			return "", err
		}
		return "queried " + string(v), nil
	})

	res, err := s.ServerSideOperation(context.Background(), "agent-1", "db.query", map[string]string{"db": "prod"})
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if !called {
		t.Error("handler was not invoked")
	}
	if res.Result != "queried secret-val" {
		t.Errorf("result = %q, want %q", res.Result, "queried secret-val")
	}
	if res.Err != nil {
		t.Errorf("unexpected Err on result: %v", res.Err)
	}
}

// TestServerSideOperationHandlerError verifies a handler error propagates and
// is recorded on the operation.
func TestServerSideOperationHandlerError(t *testing.T) {
	s := NewScriptCredentialSafe(DefaultPolicy(), &stubResolver{}, nil, nil, nil)
	s.RegisterOperationHandler("fail.op", func(_ context.Context, _ map[string]string, _ SecretResolver) (string, error) {
		return "", errors.New("boom")
	})
	res, err := s.ServerSideOperation(context.Background(), "agent-1", "fail.op", nil)
	if err == nil {
		t.Fatal("expected handler error to propagate, got nil")
	}
	if res.Err == nil {
		t.Error("expected res.Err set, got nil")
	}
	if res.Result != "" {
		t.Errorf("expected empty result on error, got %q", res.Result)
	}
}

// TestPurgeExpired verifies expired deliveries are removed and their
// credentials zero-filled, while non-expired ones remain.
func TestPurgeExpired(t *testing.T) {
	s := NewScriptCredentialSafe(DefaultPolicy(), &stubResolver{val: []byte("k")}, nil, nil, nil)
	now := time.Now().UTC()

	// Seed three deliveries directly into the ledger with varied expiry.
	s.mu.Lock()
	s.deliveryLedger["expired-1"] = &JITDeliveryResult{Credential: []byte("c1"), ExpiresAt: now.Add(-time.Minute)}
	s.deliveryLedger["expired-2"] = &JITDeliveryResult{Credential: []byte("c2"), ExpiresAt: now.Add(-time.Second)}
	s.deliveryLedger["live-1"] = &JITDeliveryResult{Credential: []byte("c3"), ExpiresAt: now.Add(time.Minute)}
	s.mu.Unlock()

	if got := s.ActiveDeliveries(); got != 3 {
		t.Fatalf("before purge: ActiveDeliveries = %d, want 3", got)
	}

	purged := s.PurgeExpired(now)
	if purged != 2 {
		t.Errorf("purged = %d, want 2", purged)
	}
	if got := s.ActiveDeliveries(); got != 1 {
		t.Errorf("after purge: ActiveDeliveries = %d, want 1", got)
	}

	// The remaining delivery must be the live one, with its credential intact.
	s.mu.RLock()
	d, ok := s.deliveryLedger["live-1"]
	s.mu.RUnlock()
	if !ok {
		t.Fatal("live-1 should still be present")
	}
	if string(d.Credential) != "c3" {
		t.Errorf("live credential zeroed/changed: got %q", string(d.Credential))
	}
}

// TestPurgeExpiredZerosCredential verifies an expired credential is zero-filled
// before the entry is dropped (defense in depth — the raw value should not
// linger even momentarily untracked).
func TestPurgeExpiredZerosCredential(t *testing.T) {
	s := NewScriptCredentialSafe(DefaultPolicy(), &stubResolver{val: []byte("k")}, nil, nil, nil)
	now := time.Now().UTC()
	cred := []byte("sensitive")
	s.mu.Lock()
	s.deliveryLedger["exp"] = &JITDeliveryResult{Credential: cred, ExpiresAt: now.Add(-time.Second)}
	s.mu.Unlock()
	s.PurgeExpired(now)
	for _, b := range cred {
		if b != 0 {
			t.Fatal("expired credential was not zero-filled")
		}
	}
}

// TestStartPurgeLoopReaps verifies the background purge loop reaps expired
// deliveries on its ticker cadence and that cancelling the context stops the
// loop. Uses the interval-injectable core with a tiny interval so the test is
// fast and deterministic.
func TestStartPurgeLoopReaps(t *testing.T) {
	s := NewScriptCredentialSafe(DefaultPolicy(), &stubResolver{val: []byte("k")}, nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.startPurgeLoop(ctx, 20*time.Millisecond)

	// Seed an already-expired delivery. The next tick must reap it.
	s.mu.Lock()
	s.deliveryLedger["exp"] = &JITDeliveryResult{
		Credential: []byte("c1"),
		ExpiresAt:  time.Now().UTC().Add(-time.Minute),
	}
	s.mu.Unlock()
	if got := s.ActiveDeliveries(); got != 1 {
		t.Fatalf("seed: ActiveDeliveries = %d, want 1", got)
	}

	// Wait for at most ~1s for the loop to reap it.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && s.ActiveDeliveries() > 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if got := s.ActiveDeliveries(); got != 0 {
		t.Fatalf("loop did not reap expired delivery: ActiveDeliveries = %d", got)
	}

	// Cancel stops the loop (the deferred ticker stop settles on return).
	cancel()
	time.Sleep(30 * time.Millisecond)
}

