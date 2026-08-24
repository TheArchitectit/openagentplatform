package patches

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/nats-io/nats.go"
	"github.com/openagentplatform/openagentplatform/pkg/agent/patcher"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// kbAgentResolver resolves an agent id to its org id. It mirrors the
// pattern used by the heartbeat/check ingest paths
// (events/heartbeat.go), which call GetAgent(ctx, "", agentID) and read
// a.OrgID.
type kbAgentResolver interface {
	GetAgent(ctx context.Context, orgID, id string) (*models.Agent, error)
}

// KBConsumer subscribes to the per-KB WinUpdate sibling subjects and
// feeds the ingest methods on the store. It owns no goroutines; the
// NATS dispatcher delivers messages synchronously to the handlers.
type KBConsumer struct {
	nc       *nats.Conn
	store    kbIngestStore
	resolver kbAgentResolver
	log      *slog.Logger
}

// kbIngestStore is the minimal store surface the consumer needs.
type kbIngestStore interface {
	IngestKBScan(ctx context.Context, orgID, agentID, kb, severity string) (string, error)
	IngestKBInstall(ctx context.Context, orgID, agentID, kb string, success, rebootRequired bool, errMsg string) (string, error)
	IngestKBRebootDone(ctx context.Context, orgID, agentID string, kbs []string) error
}

// NewKBConsumer builds a consumer. The resolver may not be nil; a nil
// store or nc yields errors at subscribe time.
func NewKBConsumer(nc *nats.Conn, store kbIngestStore, resolver kbAgentResolver, log *slog.Logger) *KBConsumer {
	if log == nil {
		log = slog.Default()
	}
	return &KBConsumer{nc: nc, store: store, resolver: resolver, log: log}
}

// Subscribe registers the wildcard subscriptions for the three per-KB
// subjects. The caller owns the returned subscriptions and must
// unsubscribe them on shutdown.
func (c *KBConsumer) Subscribe() ([]*nats.Subscription, error) {
	if c.nc == nil {
		return nil, errors.New("patches: kb consumer: no nats connection")
	}
	var subs []*nats.Subscription

	scanSub, err := c.nc.Subscribe("oap.agents.*.patch_kb.scan", func(msg *nats.Msg) {
		c.handleScan(msg)
	})
	if err != nil {
		return nil, fmt.Errorf("patches: kb consumer subscribe scan: %w", err)
	}
	subs = append(subs, scanSub)

	installSub, err := c.nc.Subscribe("oap.agents.*.patch_kb.install", func(msg *nats.Msg) {
		c.handleInstall(msg)
	})
	if err != nil {
		for _, s := range subs {
			_ = s.Unsubscribe()
		}
		return nil, fmt.Errorf("patches: kb consumer subscribe install: %w", err)
	}
	subs = append(subs, installSub)

	rebootSub, err := c.nc.Subscribe("oap.agents.*.patch_kb.reboot_done", func(msg *nats.Msg) {
		c.handleRebootDone(msg)
	})
	if err != nil {
		for _, s := range subs {
			_ = s.Unsubscribe()
		}
		return nil, fmt.Errorf("patches: kb consumer subscribe reboot_done: %w", err)
	}
	subs = append(subs, rebootSub)

	c.log.Info("kb consumer subscribed", "subjects", []string{
		"oap.agents.*.patch_kb.scan",
		"oap.agents.*.patch_kb.install",
		"oap.agents.*.patch_kb.reboot_done",
	})
	return subs, nil
}

// resolveOrg resolves an agent id to its org id via the resolver.
func (c *KBConsumer) resolveOrg(ctx context.Context, agentID string) (string, error) {
	if c.resolver == nil {
		return "", errors.New("patches: kb consumer: no agent resolver")
	}
	a, err := c.resolver.GetAgent(ctx, "", agentID)
	if err != nil {
		return "", fmt.Errorf("patches: kb consumer: resolve org for agent %s: %w", agentID, err)
	}
	if a == nil {
		return "", fmt.Errorf("patches: kb consumer: agent %s not found", agentID)
	}
	return a.OrgID, nil
}

func (c *KBConsumer) handleScan(msg *nats.Msg) {
	if msg == nil {
		return
	}
	var env patcher.PatchKBScanEnvelope
	if err := json.Unmarshal(msg.Data, &env); err != nil {
		c.log.Warn("kb scan: bad payload", "err", err)
		return
	}
	if env.AgentID == "" {
		c.log.Warn("kb scan: missing agent_id")
		return
	}
	orgID, err := c.resolveOrg(context.Background(), env.AgentID)
	if err != nil {
		c.log.Warn("kb scan: resolve org failed", "agent_id", env.AgentID, "err", err)
		return
	}
	for _, p := range env.Patches {
		kb := p.KBID
		if kb == "" {
			kb = p.Name
		}
		if kb == "" {
			// Skip rows where both KBID and Name are empty; we cannot
			// key a per-KB row without an identifier.
			continue
		}
		if _, err := c.store.IngestKBScan(context.Background(), orgID, env.AgentID, kb, p.Severity); err != nil {
			c.log.Warn("kb scan: ingest failed",
				"agent_id", env.AgentID, "kb", kb, "err", err)
		}
	}
}

func (c *KBConsumer) handleInstall(msg *nats.Msg) {
	if msg == nil {
		return
	}
	var env patcher.PatchKBInstallEnvelope
	if err := json.Unmarshal(msg.Data, &env); err != nil {
		c.log.Warn("kb install: bad payload", "err", err)
		return
	}
	if env.AgentID == "" || env.Patch == nil {
		c.log.Warn("kb install: missing agent_id or patch")
		return
	}
	orgID, err := c.resolveOrg(context.Background(), env.AgentID)
	if err != nil {
		c.log.Warn("kb install: resolve org failed", "agent_id", env.AgentID, "err", err)
		return
	}
	kb := env.Patch.KBID
	if kb == "" {
		kb = env.Patch.Name
	}
	if kb == "" {
		c.log.Warn("kb install: patch has no kb_id or name", "agent_id", env.AgentID)
		return
	}
	success := env.Result != nil && env.Result.Success
	var rebootRequired bool
	var errMsg string
	if env.Result != nil {
		rebootRequired = env.Result.RebootRequired
		errMsg = env.Result.ErrorMessage
	} else if env.Error != "" {
		errMsg = env.Error
	}
	if _, err := c.store.IngestKBInstall(context.Background(), orgID, env.AgentID, kb, success, rebootRequired, errMsg); err != nil {
		c.log.Warn("kb install: ingest failed",
			"agent_id", env.AgentID, "kb", kb, "err", err)
	}
}

func (c *KBConsumer) handleRebootDone(msg *nats.Msg) {
	if msg == nil {
		return
	}
	var env patcher.PatchKBRebootEnvelope
	if err := json.Unmarshal(msg.Data, &env); err != nil {
		c.log.Warn("kb reboot_done: bad payload", "err", err)
		return
	}
	if env.AgentID == "" {
		c.log.Warn("kb reboot_done: missing agent_id")
		return
	}
	orgID, err := c.resolveOrg(context.Background(), env.AgentID)
	if err != nil {
		c.log.Warn("kb reboot_done: resolve org failed", "agent_id", env.AgentID, "err", err)
		return
	}
	if len(env.KBs) == 0 {
		return
	}
	if err := c.store.IngestKBRebootDone(context.Background(), orgID, env.AgentID, env.KBs); err != nil {
		c.log.Warn("kb reboot_done: ingest failed",
			"agent_id", env.AgentID, "kbs", env.KBs, "err", err)
	}
}
