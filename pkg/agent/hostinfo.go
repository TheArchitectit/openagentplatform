package agent

import (
	"os"
	"runtime"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/host"
	"github.com/shirou/gopsutil/v4/mem"
)

// AgentVersion is the build-time version of the oap-agent binary.
const AgentVersion = "0.1.0"

// HostInfo describes the host the agent is running on.
type HostInfo struct {
	Hostname     string  `json:"hostname"`
	OS           string  `json:"os"`
	Platform     string  `json:"platform"`
	Arch         string  `json:"arch"`
	NumCPU       int     `json:"num_cpu"`
	TotalMemory  uint64  `json:"total_memory"`
	TotalDisk    uint64  `json:"total_disk"`
	UptimeSecs   uint64  `json:"uptime_secs"`
	CPUPercent   float64 `json:"cpu_percent"`
	MemPercent   float64 `json:"mem_percent"`
	DiskPercent  float64 `json:"disk_percent"`
	AgentVersion string  `json:"agent_version"`
}

// CollectHostInfo gathers static and dynamic host metrics.
func CollectHostInfo() (*HostInfo, error) {
	hi := &HostInfo{
		Arch:         runtime.GOARCH,
		Platform:     runtime.GOOS,
		OS:           runtime.GOOS,
		AgentVersion: AgentVersion,
	}

	if hn, err := os.Hostname(); err == nil {
		hi.Hostname = hn
	}

	hi.NumCPU = runtime.NumCPU()

	if v, err := mem.VirtualMemory(); err == nil {
		hi.TotalMemory = v.Total
		hi.MemPercent = v.UsedPercent
	}

	if u, err := host.Uptime(); err == nil {
		hi.UptimeSecs = u
	}

	// CPU and disk may fail on some platforms; non-fatal.
	if percents, err := cpu.Percent(0, false); err == nil && len(percents) > 0 {
		hi.CPUPercent = percents[0]
	}

	if d, err := disk.Usage("/"); err == nil {
		hi.TotalDisk = d.Total
		hi.DiskPercent = d.UsedPercent
	}

	return hi, nil
}
