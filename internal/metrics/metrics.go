package metrics

import (
	"fmt"
	"net"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/process"
)

type ProcessMetrics struct {
	PID        int32   `json:"pid"`
	CPUPercent float64 `json:"cpuPercent"`
	MemRSS     uint64  `json:"memRss"`
}

// CollectProcess reports CPU/memory for a single running process by PID.
// Returns an error if the process doesn't exist (e.g. already exited).
func CollectProcess(pid int) (ProcessMetrics, error) {
	p, err := process.NewProcess(int32(pid))
	if err != nil {
		return ProcessMetrics{}, err
	}
	cpuPct, _ := p.CPUPercent()
	memInfo, _ := p.MemoryInfo()
	var rss uint64
	if memInfo != nil {
		rss = memInfo.RSS
	}
	return ProcessMetrics{PID: int32(pid), CPUPercent: cpuPct, MemRSS: rss}, nil
}

type Metrics struct {
	CPUPercent     float64 `json:"cpuPercent"`
	MemUsed        uint64  `json:"memUsed"`
	MemTotal       uint64  `json:"memTotal"`
	DiskUsed       uint64  `json:"diskUsed"`
	DiskTotal      uint64  `json:"diskTotal"`
	OpenLocalPorts []int   `json:"openLocalPorts"`
}

func Collect() (Metrics, error) {
	c, _ := cpu.Percent(0, false)
	m, _ := mem.VirtualMemory()
	d, _ := disk.Usage("/")
	ports := scanLocalPorts()
	var cpuPct float64
	if len(c) > 0 {
		cpuPct = c[0]
	}
	return Metrics{CPUPercent: cpuPct, MemUsed: m.Used, MemTotal: m.Total, DiskUsed: d.Used, DiskTotal: d.Total, OpenLocalPorts: ports}, nil
}

func scanLocalPorts() []int {
	var out []int
	common := []int{80, 443, 3000, 5173, 5432, 6379, 3306, 7788}
	for _, p := range common {
		c, err := net.Dial("tcp", "127.0.0.1:"+fmt.Sprintf("%d", p))
		if err == nil {
			_ = c.Close()
			out = append(out, p)
		}
	}
	return out
}
