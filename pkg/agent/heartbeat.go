package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// HeartbeatPayload is what the agent publishes on its heartbeat subject.
type HeartbeatPayload struct {
	AgentID   string  `json:"agent_id"`
	Timestamp int64   `json:"timestamp"`
	CPU       float64 `json:"cpu_percent"`
	Memory    float64 `json:"mem_percent"`
	Disk      float64 `json:"disk_percent"`
	Uptime    uint64  `json:"uptime_secs"`
	Version   string  `json:"version"`
}

// HeartbeatSubject returns the NATS subject for a given agent's heartbeats.
func HeartbeatSubject(agentID string) string {
	return fmt.Sprintf("oap.agents.%s.heartbeat", agentID)
}

// RunHeartbeat publishes a heartbeat at the configured interval until ctx
// is cancelled. Each tick refreshes CPU/mem/disk metrics.
func RunHeartbeat(ctx context.Context, agentID string, intervalSec int, nc *NATSClient, log *slog.Logger) {
	if intervalSec <= 0 {
		intervalSec = 60
	}
	ticker := time.NewTicker(time.Duration(intervalSec) * time.Second)
	defer ticker.Stop()

	subject := HeartbeatSubject(agentID)
	log.Info("heartbeat started", "subject", subject, "interval_sec", intervalSec)

	// Fire one immediately so the platform sees us without waiting `interval`.
	publishHeartbeat(ctx, subject, agentID, nc, log)

	for {
		select {
		case <-ctx.Done():
			log.Info("heartbeat stopped")
			return
		case <-ticker.C:
			publishHeartbeat(ctx, subject, agentID, nc, log)
		}
	}
}

func publishHeartbeat(ctx context.Context, subject, agentID string, nc *NATSClient, log *slog.Logger) {
	hi, err := CollectHostInfo()
	if err != nil {
		log.Warn("hostinfo collect failed during heartbeat", "err", err)
		return
	}
	payload := HeartbeatPayload{
		AgentID:   agentID,
		Timestamp: time.Now().Unix(),
		CPU:       hi.CPUPercent,
		Memory:    hi.MemPercent,
		Disk:      hi.DiskPercent,
		Uptime:    hi.UptimeSecs,
		Version:   AgentVersion,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		log.Warn("heartbeat marshal failed", "err", err)
		return
	}
	if err := nc.Publish(ctx, subject, data); err != nil {
		log.Warn("heartbeat publish failed", "err", err)
		return
	}
}
