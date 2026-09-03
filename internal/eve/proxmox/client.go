package proxmox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/openagentplatform/openagentplatform/internal/eve"
	"github.com/openagentplatform/openagentplatform/pkg/models"
)

type ProxmoxClient struct {
	endpoint   string
	tokenID    string
	tokenSecret string
	httpClient *http.Client
}

func NewProxmoxClient(endpoint, tokenID, tokenSecret string) *ProxmoxClient {
	return &ProxmoxClient{
		endpoint:    strings.TrimSuffix(endpoint, "/"),
		tokenID:     tokenID,
		tokenSecret: tokenSecret,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *ProxmoxClient) Name() models.HypervisorProvider {
	return models.HypervisorProxmox
}

func (c *ProxmoxClient) do(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.endpoint+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "PVEAPIToken="+c.tokenID+"="+c.tokenSecret)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("proxmox: status %d: %s", resp.StatusCode, body)
	}
	return io.ReadAll(resp.Body)
}

func (c *ProxmoxClient) ListNodes(ctx context.Context) ([]eve.NodeInfo, error) {
	body, err := c.do(ctx, "/nodes")
	if err != nil {
		return nil, err
	}
	var out struct {
		Data []struct {
			Node   string  `json:"node"`
			Status string  `json:"status"`
			Uptime int     `json:"uptime"`
			CPU    float64 `json:"cpu"`
			MaxMem int     `json:"maxmem"`
			Disk   int     `json:"disk"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	var nodes []eve.NodeInfo
	for _, n := range out.Data {
		nodes = append(nodes, eve.NodeInfo{
			Name:      n.Node,
			Status:    n.Status,
			MemoryMB:  int64(n.MaxMem) / (1024 * 1024),
			UptimeSec: int64(n.Uptime),
		})
	}
	return nodes, nil
}

func (c *ProxmoxClient) ListVMs(ctx context.Context) ([]eve.VMInfo, error) {
	body, err := c.do(ctx, "/cluster/resources?type=vm")
	if err != nil {
		return nil, err
	}
	var out struct {
		Data []struct {
			VMID    int     `json:"vmid"`
			Name    string  `json:"name"`
			Status  string  `json:"status"`
			Node    string  `json:"node"`
			MaxMem  int64   `json:"maxmem"`
			MaxDisk int64   `json:"maxdisk"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	var vms []eve.VMInfo
	for _, vm := range out.Data {
		vms = append(vms, eve.VMInfo{
			ID:       fmt.Sprintf("%d", vm.VMID),
			Name:     vm.Name,
			Status:   vm.Status,
			MemoryMB: vm.MaxMem / (1024 * 1024),
			DiskGB:   vm.MaxDisk / (1024 * 1024 * 1024),
			ParentID: vm.Node,
		})
	}
	return vms, nil
}

func (c *ProxmoxClient) ListContainers(ctx context.Context) ([]eve.ContainerInfo, error) {
	body, err := c.do(ctx, "/cluster/resources?type=vm")
	if err != nil {
		return nil, err
	}
	var out struct {
		Data []struct {
			VMID    int     `json:"vmid"`
			Name    string  `json:"name"`
			Status  string  `json:"status"`
			Type    string  `json:"type"`
			Node    string  `json:"node"`
			MaxMem  int64   `json:"maxmem"`
			MaxDisk int64   `json:"maxdisk"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	var cts []eve.ContainerInfo
	for _, ct := range out.Data {
		if ct.Type != "lxc" {
			continue
		}
		cts = append(cts, eve.ContainerInfo{
			ID:       fmt.Sprintf("%d", ct.VMID),
			Name:     ct.Name,
			Status:   ct.Status,
			MemoryMB: ct.MaxMem / (1024 * 1024),
			DiskGB:   ct.MaxDisk / (1024 * 1024 * 1024),
			ParentID: ct.Node,
		})
	}
	return cts, nil
}

func (c *ProxmoxClient) ListStoragePools(ctx context.Context) ([]eve.StoragePoolInfo, error) {
	body, err := c.do(ctx, "/storage")
	if err != nil {
		return nil, err
	}
	var out struct {
		Data []struct {
			Storage   string `json:"storage"`
			Type      string `json:"type"`
			Total     int64  `json:"total"`
			Used      int64  `json:"used"`
			Available int64  `json:"avail"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	var pools []eve.StoragePoolInfo
	for _, p := range out.Data {
		pools = append(pools, eve.StoragePoolInfo{
			Name:    p.Storage,
			Type:    p.Type,
			TotalGB: p.Total / (1024 * 1024 * 1024),
			UsedGB:  p.Used / (1024 * 1024 * 1024),
		})
	}
	return pools, nil
}

func (c *ProxmoxClient) ListRecentEvents(ctx context.Context, since time.Time) ([]eve.ClusterEvent, error) {
	body, err := c.do(ctx, "/cluster/tasks?limit=50")
	if err != nil {
		return nil, err
	}
	var out struct {
		Data []struct {
			StartTime int64          `json:"starttime"`
			Type     string         `json:"type"`
			Status   string         `json:"status"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	var events []eve.ClusterEvent
	for _, t := range out.Data {
		ts := time.Unix(t.StartTime, 0)
		if ts.Before(since) {
			continue
		}
		events = append(events, eve.ClusterEvent{
			Type:      "cluster_task",
			Timestamp: ts,
			Payload:   map[string]any{"type": t.Type, "status": t.Status},
		})
	}
	return events, nil
}
