// Package memory analyzes VM sysctl parameters and cgroup version. zswap and
// zram are out of scope by design; they are never analyzed for tuning.
package memory

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/analyzer"
)

// Analyzer implements analyzer.Analyzer for the memory subsystem.
type Analyzer struct{}

// Name implements analyzer.Analyzer.
func (Analyzer) Name() string { return "memory" }

type MemInfo struct {
	Total     int64
	Available int64
	SwapTotal int64
	SwapFree  int64
}

// ParseMeminfo extracts numeric fields from /proc/meminfo (kB units).
// Exported for direct parser testing.
func ParseMeminfo(text string) MemInfo {
	vals := map[string]int64{}
	for _, ln := range strings.Split(text, "\n") {
		k, v, ok := strings.Cut(ln, ":")
		if !ok {
			continue
		}
		f := strings.Fields(strings.TrimSpace(v))
		if len(f) == 0 {
			continue
		}
		n, err := strconv.ParseInt(f[0], 10, 64)
		if err != nil {
			continue
		}
		vals[strings.TrimSpace(k)] = n
	}
	return MemInfo{
		Total:     vals["MemTotal"],
		Available: vals["MemAvailable"],
		SwapTotal: vals["SwapTotal"],
		SwapFree:  vals["SwapFree"],
	}
}

func readMeminfo() (MemInfo, error) {
	raw, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return MemInfo{}, err
	}
	return ParseMeminfo(string(raw)), nil
}

// Detect implements analyzer.Analyzer.
func (Analyzer) Detect(ctx context.Context) (analyzer.Result, error) {
	mem, err := readMeminfo()
	if err != nil {
		return analyzer.Result{}, err
	}

	gb := func(kb int64) string { return strconv.FormatInt(kb/1024/1024, 10) + " GB" }
	caps := []analyzer.Capability{
		memCap(mem),
		availCap(mem, gb),
		swapCap(mem, gb),
		swappinessCap(),
		capFromSysctl("/proc/sys/vm/vfs_cache_pressure", "vfs cache pressure", 4, 10),
		overcommitCap(),
		thpCap(),
		cgroupCap(),
	}
	return analyzer.Finalize("memory", caps, true), nil
}

func memCap(mem MemInfo) analyzer.Capability {
	return analyzer.Capability{Name: "memtotal", Value: strconv.FormatInt(mem.Available, 10) + " avail / " + strconv.FormatInt(mem.Total, 10) + " MB", Weight: 4, Status: analyzer.StatusOk}
}

// swappinessCap warns only when swap pressure is aggressive; low values are
// the desired direction for most workloads, so they must not be penalized.
func swappinessCap() analyzer.Capability {
	v, err := analyzer.ReadTrim("/proc/sys/vm/swappiness")
	if err != nil {
		return analyzer.Capability{Name: "swappiness", Value: "unavailable", Weight: 4, Status: analyzer.StatusSkip}
	}
	st := analyzer.StatusOk
	if n, err := strconv.Atoi(v); err == nil && n > 60 {
		st = analyzer.StatusWarn
	}
	return analyzer.Capability{Name: "swappiness", Value: v, Weight: 4, Status: st}
}

func availCap(mem MemInfo, gb func(int64) string) analyzer.Capability {
	if mem.Total == 0 {
		return analyzer.Capability{Name: "memavailable", Value: "unknown", Weight: 4, Status: analyzer.StatusSkip}
	}
	if mem.Available*10 < mem.Total {
		return analyzer.Capability{Name: "mem available", Value: gb(mem.Available), Weight: 4, Status: analyzer.StatusWarn}
	}
	return analyzer.Capability{Name: "mem available", Value: gb(mem.Available), Weight: 4, Status: analyzer.StatusOk}
}

func swapCap(mem MemInfo, gb func(int64) string) analyzer.Capability {
	if mem.SwapTotal == 0 {
		return analyzer.Capability{Name: "swap", Value: "off", Weight: 3, Status: analyzer.StatusOk}
	}
	return analyzer.Capability{Name: "swap", Value: gb(mem.SwapFree) + " free / " + gb(mem.SwapTotal), Weight: 3, Status: analyzer.StatusOk}
}

// capFromSysctl reads a one-line sysctl value; value "60" is a sane default
// bound, values outside warn so the admin can review.
func capFromSysctl(path, name string, weight float64, warnBelow int) analyzer.Capability {
	v, err := analyzer.ReadTrim(path)
	if err != nil {
		return analyzer.Capability{Name: name, Value: "unavailable", Weight: weight, Status: analyzer.StatusSkip}
	}
	cap := analyzer.Capability{Name: name, Value: v, Weight: weight, Status: analyzer.StatusOk}
	if n, err := strconv.Atoi(v); err == nil && n < warnBelow {
		cap.Status = analyzer.StatusWarn
	}
	return cap
}

func overcommitCap() analyzer.Capability {
	v, err := analyzer.ReadTrim("/proc/sys/vm/overcommit_memory")
	if err != nil {
		return analyzer.Capability{Name: "overcommit", Value: "unavailable", Weight: 3, Status: analyzer.StatusSkip}
	}
	st := analyzer.StatusOk
	if v != "0" {
		st = analyzer.StatusWarn
	}
	return analyzer.Capability{Name: "overcommit", Value: v, Weight: 3, Status: st}
}

func thpCap() analyzer.Capability {
	v, err := analyzer.ReadTrim("/sys/kernel/mm/transparent_hugepage/enabled")
	if err != nil {
		return analyzer.Capability{Name: "transparent hugepages", Value: "unsupported", Weight: 3, Status: analyzer.StatusSkip}
	}
	mode := v
	if i := strings.Index(v, "["); i >= 0 && strings.Index(v, "]") > i {
		mode = v[i+1 : strings.Index(v, "]")]
	}
	return analyzer.Capability{Name: "transparent hugepages", Value: mode, Weight: 3, Status: analyzer.StatusOk}
}

func cgroupCap() analyzer.Capability {
	ver := "unknown"
	if analyzer.PathExists("/sys/fs/cgroup/cgroup.controllers") {
		ver = "v2"
	} else if analyzer.PathExists("/sys/fs/cgroup/unified") {
		ver = "v2"
	} else if analyzer.PathExists("/sys/fs/cgroup/systemd") {
		ver = "v1"
	}
	return analyzer.Capability{Name: "cgroup", Value: ver, Weight: 0, Status: analyzer.StatusOk}
}
