package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"github.com/openagentplatform/openagentplatform/pkg/agent/checkers"
)

// CheckCommand is what arrives on the agent's checks subject.
type CheckCommand struct {
	CheckID  string                 `json:"check_id"`
	Type     string                 `json:"type"`
	Target   string                 `json:"target,omitempty"`
	Timeout  int                    `json:"timeout_sec,omitempty"`
	Options  map[string]interface{} `json:"options,omitempty"`
	Script   string                 `json:"script,omitempty"`
	Command  string                 `json:"command,omitempty"`
	Args     []string               `json:"args,omitempty"`
	Expected string                 `json:"expected,omitempty"`
}

// CheckResultEnvelope is the published response.
type CheckResultEnvelope struct {
	CheckID  string             `json:"check_id"`
	AgentID  string             `json:"agent_id"`
	Result   *checkers.Result   `json:"result"`
	IssuedAt int64              `json:"issued_at"`
	CompletedAt int64           `json:"completed_at"`
}

// ChecksSubject returns the NATS subject for incoming check commands.
func ChecksSubject(agentID string) string {
	return fmt.Sprintf("oap.agents.%s.checks", agentID)
}

// ChecksResultSubject returns the NATS subject for check results.
func ChecksResultSubject(agentID string) string {
	return fmt.Sprintf("oap.agents.%s.checks.result", agentID)
}

// RunChecksHandler subscribes to the checks subject and dispatches each
// message to the appropriate checker. It blocks until ctx is cancelled or
// the subscription returns an error.
func RunChecksHandler(ctx context.Context, agentID string, nc *NATSClient, log *slog.Logger) (*nats.Subscription, error) {
	subject := ChecksSubject(agentID)
	sub, err := nc.Subscribe(subject, func(msg *nats.Msg) {
		var cmd CheckCommand
		if err := json.Unmarshal(msg.Data, &cmd); err != nil {
			log.Warn("checks: bad payload", "err", err, "subject", subject)
			return
		}
		if cmd.CheckID == "" {
			cmd.CheckID = uuid.NewString()
		}

		req := &checkers.CheckRequest{
			Type:     cmd.Type,
			Target:   cmd.Target,
			Timeout:  cmd.Timeout,
			Options:  cmd.Options,
			Script:   cmd.Script,
			Command:  cmd.Command,
			Args:     cmd.Args,
			Expected: cmd.Expected,
		}

		log.Info("check received", "check_id", cmd.CheckID, "type", cmd.Type, "target", cmd.Target)
		result := checkers.Run(ctx, req)

		env := CheckResultEnvelope{
			CheckID:     cmd.CheckID,
			AgentID:     agentID,
			Result:      result,
			IssuedAt:    0,
			CompletedAt: 0,
		}
		data, err := json.Marshal(env)
		if err != nil {
			log.Warn("check result marshal failed", "err", err)
			return
		}
		if err := nc.Publish(ctx, ChecksResultSubject(agentID), data); err != nil {
			log.Warn("check result publish failed", "err", err)
		}
	})
	if err != nil {
		return nil, err
	}
	log.Info("checks handler subscribed", "subject", subject)
	return sub, nil
}
