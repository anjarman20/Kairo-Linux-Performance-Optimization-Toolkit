// Package cpu analyzes CPU topology, frequency policy and virtualization
// presence from /proc/cpuinfo and /sys. Read-only, vendor-agnostic.
package cpu

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/analyzer"
)

// Analyzer implements analyzer.Analyzer for the CPU subsystem.
type Analyzer struct{}

// Name implements analyzer.Analyzer.
func (Analyzer) Name() string { return "cpu" }

type info struct {
	Model      string
	Vendor     string
	Logical    int
	Physical   int
	Hypervisor bool
}

// ParseCPU extracts data from /proc/cpuinfo without assuming vendor or layout.
// Exported separately from the analyzer for direct parser testing.
func ParseCPU(text string) info {
	var c info
	coreIDs := map[string]struct{}{}
	for _, ln := range strings.Split(text, "\n") {
		k, v, ok := strings.Cut(ln, ":")
		if !ok {
			continue
		}
		key := strings.TrimSpace(k)
		val := strings.TrimSpace(v)
		switch key {
		case "processor":
			if n, err := strconv.Atoi(val); err == nil && n+1 > c.Logical {
				c.Logical = n + 1
			}
		case "model name":
			if val != "" {
				c.Model = val
			}
		case "Processor": // ARM fallback
			if c.Model == "" {
				c.Model = val
			}
		case "vendor_id":
			c.Vendor = val
		case "CPU implementer": // ARM vendor detection
			if c.Vendor == "" {
				c.Vendor = armVendor(val)
			}
		case "core id":
			coreIDs[val] = struct{}{}
		case "flags":
			if strings.Contains(val, "hypervisor") {
				c.Hypervisor = true
			}
		}
	}
	if len(coreIDs) > 0 {
		c.Physical = len(coreIDs)
	} else {
		c.Physical = c.Logical
	}
	return c
}

var armImplementers = map[string]string{
	"0x41": "ARM", "0x42": "Broadcom", "0x43": "Cavium", "0x44": "Marvell",
	"0x46": "Fujitsu", "0x48": "HiSilicon", "0x4d": "Motorola", "0x4e": "NVIDIA",
	"0x51": "Qualcomm", "0x53": "Samsung", "0x56": "Marvell", "0x61": "Apple",
}

func armVendor(impl string) string {
	if v, ok := armImplementers[strings.ToLower(impl)]; ok {
		return v
	}
	return "unknown (" + impl + ")"
}

// Detect implements analyzer.Analyzer.
func (Analyzer) Detect(ctx context.Context) (analyzer.Result, error) {
	raw, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return analyzer.Result{}, err
	}
	c := ParseCPU(string(raw))

	caps := []analyzer.Capability{
		{Name: "model", Value: c.Model, Weight: 4, Status: analyzer.StatusOk},
		{Name: "topology", Value: strconv.Itoa(c.Logical) + " logical / " + strconv.Itoa(c.Physical) + " physical", Weight: 4, Status: analyzer.StatusOk},
		govCap(),
		driverCap(),
		smtCap(),
		numaCap(),
	}
	if c.Hypervisor {
		caps = append(caps, analyzer.Capability{Name: "virtualized", Value: "yes", Weight: 2, Status: analyzer.StatusWarn})
	} else {
		caps = append(caps, analyzer.Capability{Name: "virtualized", Value: "no", Weight: 2, Status: analyzer.StatusOk})
	}
	return analyzer.Finalize("cpu", caps, true), nil
}

func govCap() analyzer.Capability {
	const path = "/sys/devices/system/cpu/cpu0/cpufreq/scaling_governor"
	gov, err := analyzer.ReadTrim(path)
	if err != nil {
		return analyzer.Capability{Name: "governor", Value: "unavailable", Weight: 5, Status: analyzer.StatusSkip}
	}
	if strings.Contains(gov, "performance") {
		return analyzer.Capability{Name: "governor", Value: gov, Weight: 5, Status: analyzer.StatusOk}
	}
	return analyzer.Capability{Name: "governor", Value: gov, Weight: 5, Status: analyzer.StatusWarn}
}

func driverCap() analyzer.Capability {
	drv, err := analyzer.ReadTrim("/sys/devices/system/cpu/cpu0/cpufreq/scaling_driver")
	if err != nil {
		return analyzer.Capability{Name: "scaling driver", Value: "none", Weight: 3, Status: analyzer.StatusSkip}
	}
	return analyzer.Capability{Name: "scaling driver", Value: drv, Weight: 3, Status: analyzer.StatusOk}
}

func smtCap() analyzer.Capability {
	smt, err := analyzer.ReadTrim("/sys/devices/system/cpu/smt/active")
	if err != nil {
		return analyzer.Capability{Name: "smt", Value: "unknown", Weight: 4, Status: analyzer.StatusSkip}
	}
	return analyzer.Capability{Name: "smt", Value: smt, Weight: 4, Status: analyzer.StatusOk}
}

func numaCap() analyzer.Capability {
	nodes := analyzer.Globs("/sys/devices/system/node/node[0-9]*")
	return analyzer.Capability{Name: "numa nodes", Value: strconv.Itoa(len(nodes)), Weight: 3, Status: analyzer.StatusOk}
}

//ponytail: fixed scoring distribution; rebalance when Phase 3 optimizer lands.
