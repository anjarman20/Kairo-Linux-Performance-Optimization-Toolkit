// Package benchmark runs reproducible, dependency-free performance probes for
// cpu, memory, network (loopback), storage, and latency. Results are meant for
// before/after comparison via `kairo benchmark compare`, never as absolute
// truth: every result carries skew warnings.
package benchmark

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Categories in canonical run order.
var Categories = []string{"cpu", "memory", "latency", "storage", "network"}

// SystemInfo describes the host during a measurement so comparisons can flag
// meaningful skew (virtualization, CPU scaling).
type SystemInfo struct {
	Kernel       string `json:"kernel"`
	Architecture string `json:"architecture"`
	Virtualized  bool   `json:"virtualized"`
	CPUGovernor  string `json:"cpu_governor,omitempty"`
	FrequencyKHz int64  `json:"cpu_frequency_khz,omitempty"`
}

// Result is one category's measurement plus its caveats.
type Result struct {
	Category       string     `json:"category"`
	Value          float64    `json:"value"`
	Unit           string     `json:"unit"`
	Text           string     `json:"text"`
	DurationSec    float64    `json:"duration_sec"`
	HigherIsBetter bool       `json:"higher_is_better"`
	Warnings       []string   `json:"warnings"`
	System         SystemInfo `json:"system"`
}

// Diff is one category's before/after comparison.
type Diff struct {
	Category string  `json:"category"`
	Before   float64 `json:"before"`
	After    float64 `json:"after"`
	Unit     string  `json:"unit"`
	DeltaPct float64 `json:"delta_pct"`
	Improved bool    `json:"improved"`
}

var DefaultBudget = 900 * time.Millisecond

// allCategories maps every accepted category name to its probe.
func Run(ctx context.Context, category string) (Result, error) {
	sys := systemInfo()
	switch category {
	case "cpu":
		return runCPU(ctx, DefaultBudget, sys)
	case "memory":
		return runMemory(ctx, DefaultBudget, sys)
	case "latency":
		return runLatency(ctx, DefaultBudget, sys)
	case "storage":
		return runStorage(sys, os.TempDir(), 128*1024*1024)
	case "network":
		return runNetwork(sys, 256*1024*1024)
	}
	return Result{}, fmt.Errorf("unknown benchmark category %q", category)
}

// RunSet runs the requested set: "all" (everything), "system" (everything but
// network), or a single category. Results keep canonical category order.
func RunSet(category string) ([]Result, error) {
	var cats []string
	switch category {
	case "all":
		cats = append(cats, Categories...)
	case "system":
		for _, c := range Categories {
			if c != "network" {
				cats = append(cats, c)
			}
		}
	default:
		cats = []string{category}
	}
	var out []Result
	for _, c := range cats {
		r, err := Run(context.Background(), c)
		if err != nil {
			return out, err
		}
		out = append(out, r)
	}
	return out, nil
}

// Save writes results as stable JSON, one array per file.
func Save(path string, results []Result) error {
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// Load reads a previously saved results file.
func Load(path string) ([]Result, error) {
	var out []Result
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("%s: not a kairo benchmark file: %v", path, err)
	}
	return out, nil
}

// Compare matches before/after results by category and computes deltas.
func Compare(before, after []Result) []Diff {
	byCat := func(rs []Result) map[string]Result {
		m := map[string]Result{}
		for _, r := range rs {
			if _, ok := m[r.Category]; !ok {
				m[r.Category] = r
			}
		}
		return m
	}
	b, a := byCat(before), byCat(after)
	var diffs []Diff
	for cat, bres := range b {
		ares, ok := a[cat]
		if !ok {
			continue
		}
		d := Diff{
			Category: cat,
			Before:   bres.Value,
			After:    ares.Value,
			Unit:     bres.Unit,
		}
		if bres.Value != 0 {
			d.DeltaPct = (ares.Value - bres.Value) / bres.Value * 100
		}
		d.Improved = bres.HigherIsBetter == (d.After > d.Before)
		diffs = append(diffs, d)
	}
	sort.Slice(diffs, func(i, j int) bool { return diffs[i].Category < diffs[j].Category })
	return diffs
}

// CompareFiles loads two saved runs and compares them.
func CompareFiles(beforePath, afterPath string) ([]Diff, error) {
	before, err := Load(beforePath)
	if err != nil {
		return nil, err
	}
	after, err := Load(afterPath)
	if err != nil {
		return nil, err
	}
	return Compare(before, after), nil
}

// measure runs work repeatedly until the budget elapses.
func measure(work func(), budget time.Duration) (n int, dur time.Duration) {
	start := time.Now()
	for time.Since(start) < budget {
		work()
		n++
	}
	return n, time.Since(start)
}

func systemInfo() SystemInfo {
	sys := SystemInfo{Kernel: "unknown", Architecture: runtime.GOARCH}
	if v, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
		sys.Kernel = strings.TrimSpace(string(v))
	}
	if raw, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, ln := range strings.Split(string(raw), "\n") {
			k, v, ok := strings.Cut(ln, ":")
			if !ok {
				continue
			}
			k, v = strings.TrimSpace(k), strings.TrimSpace(v)
			if k == "flags" {
				for _, f := range strings.Fields(v) {
					if f == "hypervisor" {
						sys.Virtualized = true
					}
				}
			}
		}
	}
	if v, err := os.ReadFile("/sys/devices/system/cpu/cpu0/cpufreq/scaling_governor"); err == nil {
		sys.CPUGovernor = strings.TrimSpace(string(v))
	}
	if v, err := os.ReadFile("/sys/devices/system/cpu/cpu0/cpufreq/scaling_cur_freq"); err == nil {
		var kHz int64
		if _, err := fmt.Sscanf(string(v), "%d", &kHz); err == nil {
			sys.FrequencyKHz = kHz
		}
	}
	return sys
}

// baseWarnings returns skew warnings that apply to throughput probes.
func (sys SystemInfo) baseWarnings() []string {
	var w []string
	if sys.Virtualized {
		w = append(w, "system is virtualized; results may be influenced by the host and noisy neighbors")
	}
	if sys.CPUGovernor != "" && sys.CPUGovernor != "performance" {
		w = append(w, fmt.Sprintf("CPU frequency scaling active (governor %s); results may vary between runs", sys.CPUGovernor))
	}
	return w
}

func runCPU(ctx context.Context, budget time.Duration, sys SystemInfo) (Result, error) {
	buf := make([]byte, 32*1024*1024)
	_ = sha256.Sum256(buf[:1024]) // warm up before timing

	work := func() {
		h := sha256.New()
		h.Write(buf)
		_ = h.Sum(nil)
	}
	n, dur := measure(work, budget)
	mb := float64(n*len(buf)) / (1024 * 1024) / dur.Seconds()
	return makeResult("cpu", mb, "MB/s (sha256)", true, dur, sys, nil), nil
}

func runMemory(ctx context.Context, budget time.Duration, sys SystemInfo) (Result, error) {
	const size = 64 * 1024 * 1024
	mem := make([]byte, size)
	dst := make([]byte, size)
	work := func() { copy(dst, mem) }
	n, dur := measure(work, budget)
	gb := float64(n*size) / (1024 * 1024 * 1024) / dur.Seconds()
	return makeResult("memory", gb, "GB/s (copy)", true, dur, sys, nil), nil
}

func runLatency(ctx context.Context, budget time.Duration, sys SystemInfo) (Result, error) {
	var mu sync.Mutex
	work := func() { mu.Lock(); mu.Unlock() }
	n, dur := measure(work, budget)
	ns := float64(dur.Nanoseconds()) / float64(n)
	return makeResult("latency", ns, "ns/op (mutex)", false, dur, sys, nil), nil
}

func runStorage(sys SystemInfo, dir string, size int) (Result, error) {
	payload := make([]byte, size)
	f, err := os.CreateTemp(dir, "kairo-bench-")
	if err != nil {
		return Result{}, err
	}
	name := f.Name()
	defer os.Remove(name)
	defer f.Close()

	start := time.Now()
	if _, err := f.Write(payload); err != nil {
		return Result{}, err
	}
	if err := f.Sync(); err != nil {
		return Result{}, err
	}
	writeDur := time.Since(start)

	rs, err := os.Open(name)
	if err != nil {
		return Result{}, err
	}
	defer rs.Close()
	start = time.Now()
	got, err := io.Copy(io.Discard, rs)
	if err != nil {
		return Result{}, err
	}
	readDur := time.Since(start)

	total := float64(got) / (1024 * 1024)
	w := sys.baseWarnings()
	w = append(w, "storage reads may be served from page cache; first run after a host restart is slower")
	res := Result{
		Category:       "storage",
		Value:          total / readDur.Seconds(),
		Unit:           "MB/s (read)",
		Text:           fmt.Sprintf("write %.0f MB/s, read %.0f MB/s", total/writeDur.Seconds(), total/readDur.Seconds()),
		DurationSec:    readDur.Seconds(),
		HigherIsBetter: true,
		Warnings:       w,
		System:         sys,
	}
	return res, nil
}

func runNetwork(sys SystemInfo, size int) (Result, error) {
	const chunk = 256 * 1024
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Result{}, fmt.Errorf("network benchmark requires loopback: %w", err)
	}
	defer ln.Close()

	payload := make([]byte, chunk)
	received := make(chan int64, 1)
	serveErr := make(chan error, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			serveErr <- err
			return
		}
		defer c.Close()
		n, err := io.Copy(io.Discard, c)
		if err != nil {
			serveErr <- err
			return
		}
		received <- n
	}()

	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		return Result{}, err
	}
	start := time.Now()
	var written int64
	for written < int64(size) {
		n, err := c.Write(payload)
		if err != nil {
			c.Close()
			return Result{}, err
		}
		written += int64(n)
	}
	c.Close()

	var got int64
	select {
	case got = <-received:
	case err := <-serveErr:
		return Result{}, err
	case <-time.After(30 * time.Second):
		return Result{}, fmt.Errorf("network benchmark timed out")
	}
	dur := time.Since(start)

	w := sys.baseWarnings()
	w = append(w, "loopback network path differs from real links; use for relative before/after only")
	mb := float64(got) / (1024 * 1024) / dur.Seconds()
	return makeResult("network", mb, "MB/s (loopback)", true, dur, sys, w), nil
}

func makeResult(cat string, value float64, unit string, higher bool, dur time.Duration, sys SystemInfo, extra []string) Result {
	w := sys.baseWarnings()
	w = append(w, extra...)
	if dur < 500*time.Millisecond {
		w = append(w, "test duration under 0.5s; sample may be small")
	}
	return Result{
		Category:       cat,
		Value:          value,
		Unit:           unit,
		Text:           fmt.Sprintf("%.1f %s", value, unit),
		DurationSec:    dur.Seconds(),
		HigherIsBetter: higher,
		Warnings:       w,
		System:         sys,
	}
}
