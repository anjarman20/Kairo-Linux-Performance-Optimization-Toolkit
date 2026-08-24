// Package network analyzes NIC, qdisc, congestion control and packet-steering
// state. It never modifies network configuration and never changes MTU.
package network

import (
	"context"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/analyzer"
)

// Analyzer implements analyzer.Analyzer for the network subsystem.
type Analyzer struct{}

// Name implements analyzer.Analyzer.
func (Analyzer) Name() string { return "network" }

// ParseRoute extracts the interface carrying the IPv4 default route from
// /proc/net/route: destination and mask both zero (00000000). The loopback
// catch-all entry is skipped. Exported for parser testing.
func ParseRoute(text string) string {
	for _, ln := range strings.Split(text, "\n") {
		f := strings.Fields(ln)
		if len(f) < 8 || f[0] == "Iface" || f[0] == "lo" {
			continue
		}
		if f[1] == "00000000" && f[7] == "00000000" {
			return f[0]
		}
	}
	return ""
}

func activeInterface() string {
	raw, err := os.ReadFile("/proc/net/route")
	if err == nil {
		if iface := ParseRoute(string(raw)); iface != "" {
			return iface
		}
	}
	ifaces := analyzer.Globs("/sys/class/net/*")
	sort.Strings(ifaces)
	for _, p := range ifaces {
		name := strings.TrimPrefix(p, "/sys/class/net/")
		if name == "lo" {
			continue
		}
		op, err := analyzer.ReadTrim(p + "/operstate")
		if err == nil && op == "up" {
			return name
		}
	}
	return ""
}

// queueEnabled scores a packet-steering mask: non-zero cpumask means enabled.
func queueEnabled(glob, name string, weight float64) analyzer.Capability {
	mask, err := analyzer.ReadTrim(glob)
	if err != nil {
		return analyzer.Capability{Name: name, Value: "disabled", Weight: weight, Status: analyzer.StatusWarn}
	}
	if strings.Trim(mask, "0") == "" {
		return analyzer.Capability{Name: name, Value: "disabled", Weight: weight, Status: analyzer.StatusWarn}
	}
	return analyzer.Capability{Name: name, Value: "enabled (" + mask + ")", Weight: weight, Status: analyzer.StatusOk}
}

// Detect implements analyzer.Analyzer.
func (Analyzer) Detect(ctx context.Context) (analyzer.Result, error) {
	iface := activeInterface()

	caps := []analyzer.Capability{}
	if iface == "" {
		caps = append(caps, analyzer.Capability{Name: "active interface", Value: "none", Weight: 4, Status: analyzer.StatusSkip})
		base := analyzer.Finalize("network", caps, true)
		base.Summary = "no active interface detected"
		return base, nil
	}
	caps = append(caps, analyzer.Capability{Name: "active interface", Value: iface, Weight: 4, Status: analyzer.StatusOk})

	if mtu, err := analyzer.ReadTrim("/sys/class/net/" + iface + "/mtu"); err == nil {
		caps = append(caps, analyzer.Capability{Name: "mtu", Value: mtu + " B", Weight: 3, Status: analyzer.StatusOk})
	} else {
		caps = append(caps, analyzer.Capability{Name: "mtu", Value: "unknown", Weight: 3, Status: analyzer.StatusSkip})
	}

	caps = append(caps, qdiscCap(ctx, iface))
	caps = append(caps, ccCap())
	caps = append(caps, ccAvailCap())
	caps = append(caps, queueEnabled("/sys/class/net/"+iface+"/queues/rx-0/rps_cpus", "rps", 3))
	caps = append(caps, queueEnabled("/sys/class/net/"+iface+"/queues/tx-0/xps_cpus", "xps", 2))

	return analyzer.Finalize("network", caps, true), nil
}

func qdiscCap(ctx context.Context, iface string) analyzer.Capability {
	out, err := analyzer.Run(ctx, 3*time.Second, "tc", "qdisc", "show", "dev", iface)
	if err != nil {
		return analyzer.Capability{Name: "qdisc", Value: "unavailable (tc missing)", Weight: 5, Status: analyzer.StatusSkip}
	}
	kind := ""
	for _, ln := range strings.Split(out, "\n") {
		f := strings.Fields(ln)
		for i, t := range f {
			if t == "qdisc" && i+1 < len(f) && kind == "" {
				kind = f[i+1]
			}
		}
	}
	return analyzer.Capability{Name: "qdisc", Value: kind, Weight: 5, Status: analyzer.StatusOk}
}

func ccCap() analyzer.Capability {
	v, err := analyzer.ReadTrim("/proc/sys/net/ipv4/tcp_congestion_control")
	if err != nil {
		return analyzer.Capability{Name: "congestion control", Value: "unavailable", Weight: 5, Status: analyzer.StatusSkip}
	}
	return analyzer.Capability{Name: "congestion control", Value: v, Weight: 5, Status: analyzer.StatusOk}
}

func ccAvailCap() analyzer.Capability {
	v, err := analyzer.ReadTrim("/proc/sys/net/ipv4/tcp_available_congestion_control")
	if err != nil {
		return analyzer.Capability{Name: "available CC", Value: "unavailable", Weight: 3, Status: analyzer.StatusSkip}
	}
	return analyzer.Capability{Name: "available CC", Value: v, Weight: 3, Status: analyzer.StatusOk}
}
