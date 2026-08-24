// Package network analyzes NIC, qdisc, congestion control and packet-steering
// state. It never modifies network configuration and never changes MTU.
package network

import (
	"context"
	"fmt"
	"math/bits"
	"os"
	"sort"
	"strconv"
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

// queueUtil summarizes steering (RPS/XPS) across every queue of one type.
func queueUtil(iface, dir, maskFile string) (enabled, total int) {
	for _, p := range analyzer.Globs("/sys/class/net/" + iface + "/queues/" + dir + "-*/" + maskFile) {
		total++
		if m, err := analyzer.ReadTrim(p); err == nil && strings.Trim(m, "0") != "" {
			enabled++
		}
	}
	return enabled, total
}

// steeringCap aggregates RPS/XPS health across all queues of the interface.
func steeringCap(iface, dir, maskFile, name string, weight float64) analyzer.Capability {
	enabled, total := queueUtil(iface, dir, maskFile)
	cap := analyzer.Capability{Name: name, Value: fmt.Sprintf("%d/%d queues active", enabled, total), Weight: weight, Status: analyzer.StatusOk}
	if total > 0 && enabled < total {
		cap.Status = analyzer.StatusWarn // partial steering needs review
	}
	if total == 0 {
		cap.Value = "no queues detected"
		cap.Status = analyzer.StatusSkip
	}
	return cap
}

// ParseIrqs returns the IRQ numbers whose /proc/interrupts entry names iface.
func ParseIrqs(text, iface string) []int {
	var irqs []int
	for _, ln := range strings.Split(text, "\n") {
		if !strings.Contains(ln, iface) {
			continue
		}
		f := strings.Fields(ln)
		if len(f) < 2 {
			continue
		}
		irq := strings.TrimSuffix(f[0], ":")
		n, err := strconv.Atoi(irq)
		if err != nil {
			continue
		}
		irqs = append(irqs, n)
	}
	return irqs
}

// AffinityCoversAll reports whether the hex cpumask spans every CPU: either
// every bit up to the CPU count is set, or the mask is the all-ff saturation
// value the kernel writes when affinity has no useful restriction.
func AffinityCoversAll(maskHex string, cpus int) bool {
	m := strings.ReplaceAll(strings.TrimSpace(maskHex), ",", "")
	if m == "" {
		return false
	}
	if !isHex(m) {
		return false
	}
	if cpus > 0 && bits.OnesCount64(maskValue(m)) >= cpus {
		return true
	}
	allFF := true
	for _, ch := range m {
		if ch != 'f' && ch != 'F' {
			allFF = false
			break
		}
	}
	return allFF
}

func maskValue(m string) uint64 {
	v, _ := strconv.ParseUint(m, 16, 64)
	return v
}

func isHex(m string) bool {
	for _, ch := range m {
		if !strings.ContainsRune("0123456789abcdefABCDEF", ch) {
			return false
		}
	}
	return true
}

// countCPUs derives online CPU count from /proc/cpuinfo.
func countCPUs() int {
	raw, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return 0
	}
	n := 0
	for _, ln := range strings.Split(string(raw), "\n") {
		if k, v, ok := strings.Cut(ln, ":"); ok && strings.TrimSpace(k) == "processor" && strings.TrimSpace(v) != "" {
			n++
		}
	}
	return n
}

// irqAffinityCap inspects /proc/irq/<n>/smp_affinity for the interface IRQs.
func irqAffinityCap(ctx context.Context, iface string) analyzer.Capability {
	raw, err := os.ReadFile("/proc/interrupts")
	if err != nil {
		return analyzer.Capability{Name: "irq affinity", Value: "unavailable", Weight: 0, Status: analyzer.StatusSkip}
	}
	irqs := ParseIrqs(string(raw), iface)
	if len(irqs) == 0 {
		return analyzer.Capability{Name: "irq affinity", Value: "no owning IRQs found", Weight: 0, Status: analyzer.StatusSkip}
	}
	cpus := countCPUs()
	var distinct []string
	wide := false
	for _, n := range irqs {
		mask, err := analyzer.ReadTrim(fmt.Sprintf("/proc/irq/%d/smp_affinity", n))
		if err != nil {
			continue
		}
		distinct = append(distinct, mask)
		if AffinityCoversAll(mask, cpus) {
			wide = true
		}
	}
	if len(distinct) == 0 {
		return analyzer.Capability{Name: "irq affinity", Value: "owning IRQs have no affinity files", Weight: 0, Status: analyzer.StatusSkip}
	}
	st := analyzer.StatusOk
	if wide {
		st = analyzer.StatusWarn
	}
	return analyzer.Capability{Name: "irq affinity", Value: strings.Join(distinct, " "), Weight: 0, Status: st}
}

// offloadCap summarizes GRO/GSO/TSO state from `ethtool -k`. Offloads are
// reported but never scored and never disabled automatically.
func offloadCap(ctx context.Context, iface string) analyzer.Capability {
	out, err := analyzer.Run(ctx, 3*time.Second, "ethtool", "-k", iface)
	if err != nil {
		return analyzer.Capability{Name: "offloads", Value: "unavailable (ethtool)", Weight: 0, Status: analyzer.StatusSkip}
	}
	want := []string{"gro", "gso", "tso"}
	var parts []string
	for _, w := range want {
		for _, ln := range strings.Split(out, "\n") {
			k, v, ok := strings.Cut(ln, ":")
			if ok && strings.TrimSpace(k) == w {
				parts = append(parts, w+"="+strings.TrimSpace(v))
				break
			}
		}
	}
	if len(parts) == 0 {
		return analyzer.Capability{Name: "offloads", Value: "no offload keys parsed", Weight: 0, Status: analyzer.StatusSkip}
	}
	return analyzer.Capability{Name: "offloads", Value: strings.Join(parts, " "), Weight: 0, Status: analyzer.StatusOk}
}

// ringCap parses current RX/TX ring sizes from `ethtool -g`.
func ringCap(ctx context.Context, iface string) analyzer.Capability {
	out, err := analyzer.Run(ctx, 3*time.Second, "ethtool", "-g", iface)
	if err != nil {
		return analyzer.Capability{Name: "ring buffers", Value: "unavailable (ethtool)", Weight: 0, Status: analyzer.StatusSkip}
	}
	section := ""
	var rx, tx string
	for _, ln := range strings.Split(out, "\n") {
		t := strings.TrimSpace(ln)
		switch {
		case t == "Current hardware settings:":
			section = "current"
		case t == "Pre-set maximums:":
			section = "max"
		}
		if section != "current" {
			continue
		}
		f := strings.Fields(t)
		if len(f) == 2 && (f[0] == "RX:" || f[0] == "TX:") {
			if f[0] == "RX:" {
				rx = f[1]
			} else {
				tx = f[1]
			}
		}
	}
	if rx == "" || tx == "" {
		return analyzer.Capability{Name: "ring buffers", Value: "unavailable", Weight: 0, Status: analyzer.StatusSkip}
	}
	return analyzer.Capability{Name: "ring buffers", Value: "rx=" + rx + " tx=" + tx, Weight: 0, Status: analyzer.StatusOk}
}

// Detect implements analyzer.Analyzer.
func (Analyzer) Detect(ctx context.Context) (analyzer.Result, error) {
	iface := activeInterface()

	caps := []analyzer.Capability{}
	if iface == "" {
		caps = append(caps, analyzer.Capability{Name: "active interface", Value: "none", Weight: 4, Status: analyzer.StatusSkip})
		base := analyzer.Finalize("network", caps, true)
		base.Status = analyzer.StatusSkip
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
	caps = append(caps, steeringCap(iface, "rx", "rps_cpus", "rps", 3))
	caps = append(caps, steeringCap(iface, "tx", "xps_cpus", "xps", 2))
	caps = append(caps, irqAffinityCap(ctx, iface))
	caps = append(caps, offloadCap(ctx, iface))
	caps = append(caps, ringCap(ctx, iface))

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
