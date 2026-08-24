// Package kernel reports kernel version, feature availability and
// virtualization detection. Informational, never scored, read-only.
package kernel

import (
	"context"
	"os"
	"runtime"
	"strings"

	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/analyzer"
)

// Analyzer implements analyzer.Analyzer for the kernel subsystem.
type Analyzer struct{}

// Name implements analyzer.Analyzer.
func (Analyzer) Name() string { return "kernel" }

func virtualized() string {
	if raw, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, ln := range strings.Split(string(raw), "\n") {
			k, v, ok := strings.Cut(ln, ":")
			if !ok || strings.TrimSpace(k) != "flags" {
				continue
			}
			for _, f := range strings.Fields(v) {
				if f == "hypervisor" {
					return "yes (hypervisor flag)"
				}
			}
		}
	}
	if v, err := analyzer.ReadTrim("/sys/class/dmi/id/product_name"); err == nil {
		n := strings.ToLower(v)
		for _, hv := range []string{"kvm", "qemu", "vmware", "xen", "virtualbox", "virtual machine", "vbox"} {
			if strings.Contains(n, hv) {
				return "yes (" + v + ")"
			}
		}
	}
	return "no"
}

// Detect implements analyzer.Analyzer.
func (Analyzer) Detect(ctx context.Context) (analyzer.Result, error) {
	version, err := analyzer.ReadTrim("/proc/sys/kernel/osrelease")
	if err != nil {
		return analyzer.Result{}, err
	}
	cc, err1 := analyzer.ReadTrim("/proc/sys/net/ipv4/tcp_available_congestion_control")
	if err1 != nil {
		cc = "unavailable"
	}
	qdisc, err2 := analyzer.ReadTrim("/proc/sys/net/core/default_qdisc")
	if err2 != nil {
		qdisc = "unavailable"
	}

	caps := []analyzer.Capability{
		{Name: "kernel", Value: version, Weight: 0, Status: analyzer.StatusOk},
		{Name: "architecture", Value: runtime.GOARCH, Weight: 0, Status: analyzer.StatusOk},
		{Name: "default qdisc", Value: qdisc, Weight: 0, Status: analyzer.StatusOk},
		{Name: "congestion control", Value: cc, Weight: 0, Status: analyzer.StatusOk},
		{Name: "virtualized", Value: virtualized(), Weight: 0, Status: analyzer.StatusOk},
	}
	if strings.Contains(cc, "bbr") {
		caps = append(caps, analyzer.Capability{Name: "BBR support", Value: "yes", Weight: 0, Status: analyzer.StatusOk})
	} else {
		caps = append(caps, analyzer.Capability{Name: "BBR support", Value: "no", Weight: 0, Status: analyzer.StatusWarn})
	}
	return analyzer.Finalize("kernel", caps, false), nil
}
