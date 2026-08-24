// Package inject provides the credential injection pipeline for OAP agents.
//
// Once a SecretResolver has fetched a secret value, the injector determines
// the safest way to deliver it to the calling agent process — as an
// environment variable, a short-lived temp file, or a one-shot stdin pipe —
// and tracks each injection for TTL-based cleanup.
package inject

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/openagentplatform/openagentplatform/internal/audit"
	"github.com/openagentplatform/openagentplatform/secrets/resolver"
)

// InjectMethod identifies how a credential is delivered to the agent.
type InjectMethod string

const (
	// MethodEnv sets a process environment variable.
	MethodEnv InjectMethod = "env"
	// MethodFile writes a temp file (mode 0600) with the credential.
	MethodFile InjectMethod = "file"
	// MethodStdin pipes the credential through a one-shot unix socket/named pipe.
	MethodStdin InjectMethod = "stdin"
)

// InjectionSpec describes a single planned credential delivery.
type InjectionSpec struct {
	// Method is the delivery method.
	Method InjectMethod
	// Key is the logical name of the secret (e.g. "ssh-key", "api-token").
	Key string
	// Value is the resolved secret bytes.
	Value []byte
	// Mode is the file mode for file-based injection (default 0600).
	Mode os.FileMode
	// TTL is how long the injection may live before the sweeper cleans it up.
	TTL time.Duration
	// URI is the original ref:oap:// URI the value was resolved from.
	URI string
	// LeaseID is set for dynamic secrets whose lease must be revoked on cleanup.
	LeaseID string
	// LeaseBackend is the backend that issued the dynamic lease.
	LeaseBackend string
	// agentID is the owning agent (used for file path naming).
	agentID string
}

// AgentID returns the agent identifier associated with this injection.
// Used by file and stdin injectors to namespace temp files / pipes.
func (s InjectionSpec) AgentID() string {
	return s.agentID
}

// SetAgentID attaches an agent identifier to the injection spec.
func (s *InjectionSpec) SetAgentID(id string) {
	s.agentID = id
}

// InjectionResult is the outcome of a single executed injection.
type InjectionResult struct {
	Spec InjectionSpec
	// Path is the filesystem path for file injections, or env-var name for env
	// injections, or pipe path for stdin injections.
	Path string
	Err  error
}

// Injector plans, executes, and cleans up credential injections for a single
// agent invocation.
type Injector struct {
	mu     sync.Mutex
	specs  []InjectionSpec
	env    *envInjector
	file   *fileInjector
	stdin  *stdinInjector
	r      *resolver.SecretResolver
	auth   *resolver.AuthContext
	logger *slog.Logger
	audit  *audit.AuditService
}

// NewInjector creates a CredentialInjector bound to a resolver.
func NewInjector(r *resolver.SecretResolver, auth *resolver.AuthContext, logger *slog.Logger, auditSvc *audit.AuditService) *Injector {
	if logger == nil {
		logger = slog.Default()
	}
	return &Injector{
		env:    newEnvInjector(),
		file:   newFileInjector(),
		stdin:  newStdinInjector(),
		r:      r,
		auth:   auth,
		logger: logger,
		audit:  auditSvc,
	}
}

// Plan resolves every URI and assigns an injection method based on the secret
// metadata and key heuristics. SSH keys and certificates default to file
// injection; short one-time tokens default to stdin; everything else goes to
// the environment.
func (in *Injector) Plan(ctx context.Context, agentID string, uris []string) ([]InjectionSpec, error) {
	specs := make([]InjectionSpec, 0, len(uris))

	for _, uri := range uris {
		val, err := in.r.Resolve(ctx, uri, in.auth)
		if err != nil {
			return nil, fmt.Errorf("inject: resolve %s: %w", uri, err)
		}

		method := pickMethod(agentID, val)

		raw, err := encodeValue(val)
		if err != nil {
			return nil, fmt.Errorf("inject: encode %s: %w", uri, err)
		}

		ttl := defaultTTL
		if val.Metadata.IsDynamic && val.Metadata.LeaseDuration > 0 {
			ttl = val.Metadata.LeaseDuration
		}

		spec := InjectionSpec{
			Method:       method,
			Key:          extractKey(val, uri),
			Value:        raw,
			Mode:         0o600,
			TTL:          ttl,
			URI:          uri,
			LeaseID:      val.Metadata.LeaseID,
			LeaseBackend: backendFromURI(uri),
			agentID:      agentID,
		}
		specs = append(specs, spec)
	}

	in.mu.Lock()
	in.specs = append(in.specs, specs...)
	in.mu.Unlock()

	return specs, nil
}

// Execute runs each spec through the matching injector and collects results.
func (in *Injector) Execute(ctx context.Context, specs []InjectionSpec) []InjectionResult {
	results := make([]InjectionResult, 0, len(specs))

	for _, s := range specs {
		var (
			path string
			err  error
		)

		switch s.Method {
		case MethodEnv:
			path, err = in.env.inject(s)
		case MethodFile:
			path, err = in.file.inject(s)
		case MethodStdin:
			path, err = in.stdin.inject(s)
		default:
			err = fmt.Errorf("inject: unknown method %q", s.Method)
		}

		results = append(results, InjectionResult{Spec: s, Path: path, Err: err})

		if err == nil {
			in.emitAudit(ctx, "secret.inject", s.URI, s.Key, s.Method, audit.OutcomeSuccess, "")
		} else {
			in.emitAudit(ctx, "secret.inject", s.URI, s.Key, s.Method, audit.OutcomeError, err.Error())
		}
	}

	return results
}

// Cleanup removes every injection this injector has tracked: temp files are
// securely wiped and unlinked, env vars are unset, and any dynamic secret
// leases are revoked through the originating backend.
func (in *Injector) Cleanup(ctx context.Context) {
	in.mu.Lock()
	specs := in.specs
	in.specs = nil
	in.mu.Unlock()

	for i := range specs {
		s := &specs[i]
		switch s.Method {
		case MethodEnv:
			if err := in.env.cleanup(*s); err != nil {
				in.logger.Warn("env cleanup failed", "key", s.Key, "err", err)
			}
		case MethodFile:
			if err := in.file.cleanup(*s); err != nil {
				in.logger.Warn("file cleanup failed", "key", s.Key, "err", err)
			}
		case MethodStdin:
			if err := in.stdin.cleanup(*s); err != nil {
				in.logger.Warn("stdin cleanup failed", "key", s.Key, "err", err)
			}
		}

		if s.LeaseID != "" {
			in.revokeLease(ctx, *s)
		}

		// Zero the credential bytes in the original slice entry.
		for j := range specs[i].Value {
			specs[i].Value[j] = 0
		}
	}
}

// revokeLease revokes a dynamic-secret lease through the appropriate backend.
func (in *Injector) revokeLease(ctx context.Context, s InjectionSpec) {
	if in.r == nil {
		return
	}
	be, ok := in.r.BackendFor(s.LeaseBackend)
	if !ok {
		in.logger.Warn("lease revoke: backend not found", "backend", s.LeaseBackend)
		return
	}
	if err := be.RevokeLease(ctx, s.LeaseID); err != nil {
		in.logger.Warn("lease revoke failed", "lease_id", s.LeaseID, "err", err)
		in.emitAudit(ctx, "secret.lease.revoke", s.URI, s.LeaseID, s.Method, audit.OutcomeError, err.Error())
		return
	}
	in.emitAudit(ctx, "secret.lease.revoke", s.URI, s.LeaseID, s.Method, audit.OutcomeSuccess, "")
}

func (in *Injector) emitAudit(ctx context.Context, action, resourceID, detail string, method InjectMethod, outcome audit.Outcome, errMsg string) {
	if in.audit == nil {
		return
	}
	details := map[string]any{
		"method": string(method),
		"detail": detail,
	}
	if errMsg != "" {
		details["error"] = errMsg
	}
	actorID := ""
	if in.auth != nil {
		actorID = in.auth.AgentID
	}
	_, _ = in.audit.Record(ctx, audit.EventInput{
		ActorType:    audit.ActorSystem,
		ActorID:      actorID,
		Action:       action,
		ResourceType: "secret",
		ResourceID:   resourceID,
		Details:      details,
		Outcome:      outcome,
	})
}
