package analyzer_test

import (
	"testing"

	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/analyzer"
	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/analyzer/cpu"
	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/analyzer/memory"
	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/analyzer/network"
)

func TestScoreCaps(t *testing.T) {
	caps := []analyzer.Capability{
		{Weight: 10, Status: analyzer.StatusOk},
		{Weight: 10, Status: analyzer.StatusWarn},
		{Weight: 10, Status: analyzer.StatusSkip},
	}
	got, max := analyzer.ScoreCaps(caps)
	if got != 15 {
		t.Fatalf("ScoreCaps got=%v want=15", got)
	}
	if max != 30 {
		t.Fatalf("ScoreCaps max=%v want=30", max)
	}
}

func TestParseCPU(t *testing.T) {
	raw := `processor	: 0
vendor_id	: GenuineIntel
model name	: Intel(R) Core(TM) i7-8700K CPU @ 3.70GHz
core id		: 0
processor	: 1
core id		: 1
flags		: fpu vme ss hypervisor
`
	info := cpu.ParseCPU(raw)
	if info.Logical != 2 {
		t.Errorf("Logical=%d want=2", info.Logical)
	}
	if info.Physical != 2 {
		t.Errorf("Physical=%d want=2", info.Physical)
	}
	if !info.Hypervisor {
		t.Error("hypervisor flag expected")
	}
}

func TestParseCPUArmNoCoreID(t *testing.T) {
	raw := `processor	: 0
CPU implementer	: 0x48
Processor	: HiSilicon Kunpeng 920
processor	: 1
`
	info := cpu.ParseCPU(raw)
	if info.Model != "HiSilicon Kunpeng 920" {
		t.Errorf("Model=%q", info.Model)
	}
	if info.Vendor != "HiSilicon" {
		t.Errorf("Vendor=%q want=HiSilicon", info.Vendor)
	}
	if info.Physical != info.Logical {
		t.Errorf("Physical=%d Logical=%d, want equal fallback", info.Physical, info.Logical)
	}
}

func TestParseMeminfo(t *testing.T) {
	raw := `MemTotal:       16298992 kB
MemAvailable:    9123456 kB
SwapTotal:       2097152 kB
SwapFree:        1048576 kB
`
	m := memory.ParseMeminfo(raw)
	if m.Total != 16298992 {
		t.Errorf("Total=%d", m.Total)
	}
	if m.Available != 9123456 {
		t.Errorf("Available=%d", m.Available)
	}
	if m.SwapFree != 1048576 {
		t.Errorf("SwapFree=%d", m.SwapFree)
	}
}

func TestParseRoute(t *testing.T) {
	raw := `Iface	Destination	Gateway 	Flags	RefCnt	Use	Metric	Mask		MTU	Window	IRTT
eth0	00000000	0100000A	0003	0	0	0	00000000	0	0	0
eth0	0100000A	00000000	0001	0	0	0	00FFFFFF	0	0	0
`
	if got := network.ParseRoute(raw); got != "eth0" {
		t.Errorf("route iface=%q want=eth0", got)
	}
	if got := network.ParseRoute("Iface\tDestination\n"); got != "" {
		t.Errorf("empty route=%q want=\"\"", got)
	}
}

func TestParseRouteSkipsLoopbackAndGatelessWarning(t *testing.T) {
	// lo catch-all must never win; default route without a gateway (flags 0001)
	// is still a valid default route on point-to-point links.
	raw := `Iface	Destination	Gateway 	Flags	RefCnt	Use	Metric	Mask		MTU	Window	IRTT
lo	00000000	00000000	0001	0	0	0	00000000	0	0	0
eth0	00000000	00000000	0001	0	0	0	00000000	0	0	0
`
	if got := network.ParseRoute(raw); got != "eth0" {
		t.Errorf("route iface=%q want=eth0", got)
	}
}

func TestParseIrqs(t *testing.T) {
	raw := `           CPU0       CPU1
  0:         11          0   IO-APIC   2-edge      timer
 24:         42         37   PCI-MSI 33554465-edge      ens3-TxRx-0
 25:         10         55   PCI-MSI 33554466-edge      ens3-TxRx-1
 26:          0          0   PCI-MSI 33554467-edge      igbvf
`
	got := network.ParseIrqs(raw, "ens3")
	if len(got) != 2 || got[0] != 24 || got[1] != 25 {
		t.Errorf("ParseIrqs(ens3)=%v want [24 25]", got)
	}
	if len(network.ParseIrqs(raw, "eth0")) != 0 {
		t.Error("unrelated interface must match nothing")
	}
}

func TestAffinityCoversAll(t *testing.T) {
	cases := []struct {
		mask string
		cpus int
		want bool
	}{
		{"f", 4, true},     // all 4 bits
		{"3", 4, false},    // only two CPUs
		{"ff,ff", 2, true}, // saturated 64-bit mask => unrestricted/broadcast
		{"f", 64, true},    // saturated all-ff
		{"4", 4, false},    // single CPU
	}
	for _, c := range cases {
		if got := network.AffinityCoversAll(c.mask, c.cpus); got != c.want {
			t.Errorf("AffinityCoversAll(%q,%d)=%v want=%v", c.mask, c.cpus, got, c.want)
		}
	}
}

func BenchmarkScoreCaps(b *testing.B) {
	caps := []analyzer.Capability{
		{Weight: 5, Status: analyzer.StatusOk},
		{Weight: 5, Status: analyzer.StatusWarn},
	}
	for i := 0; i < b.N; i++ {
		analyzer.ScoreCaps(caps)
	}
}
