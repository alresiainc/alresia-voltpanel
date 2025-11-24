package metrics

import (
	"fmt"
	"net"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
)

type Metrics struct {
	CPUPercent float64 `json:"cpuPercent"`
	MemUsed uint64 `json:"memUsed"`
	MemTotal uint64 `json:"memTotal"`
	DiskUsed uint64 `json:"diskUsed"`
	DiskTotal uint64 `json:"diskTotal"`
	OpenLocalPorts []int `json:"openLocalPorts"`
}

func Collect() (Metrics, error) {
	c, _ := cpu.Percent(0, false)
	m, _ := mem.VirtualMemory()
	d, _ := disk.Usage("/")
	ports := scanLocalPorts()
	var cpuPct float64
	if len(c) > 0 { cpuPct = c[0] }
	return Metrics{CPUPercent: cpuPct, MemUsed: m.Used, MemTotal: m.Total, DiskUsed: d.Used, DiskTotal: d.Total, OpenLocalPorts: ports}, nil
}

func scanLocalPorts() []int {
	var out []int
	for p := 1; p <= 65535 && len(out) < 50; p++ {
		ln, err := net.Listen("tcp", "127.0.0.1:"+fmt.Sprintf("%d", p))
		if err != nil { continue }
		_ = ln.Close()
		// port was free; we only collect open ports by scanning in reverse using Dial
	}
	// quick scan of common ports
	common := []int{80, 443, 3000, 5173, 5432, 6379, 3306, 7788}
	for _, p := range common {
		c, err := net.Dial("tcp", "127.0.0.1:"+fmt.Sprintf("%d", p))
		if err == nil { _ = c.Close(); out = append(out, p) }
	}
	return out
}
