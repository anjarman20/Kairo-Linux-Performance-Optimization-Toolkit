package benchmark

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRunCPUAndKind(t *testing.T) {
	r, err := runCPU(context.Background(), 20*time.Millisecond, SystemInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Category != "cpu" || r.Value <= 0 {
		t.Errorf("cpu result bad: %+v", r)
	}
	if !r.HigherIsBetter {
		t.Error("cpu must be higher-is-better")
	}
}

func TestRunMemory(t *testing.T) {
	r, err := runMemory(context.Background(), 20*time.Millisecond, SystemInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Value <= 0 {
		t.Errorf("memory result bad: %+v", r)
	}
}

func TestRunLatency(t *testing.T) {
	r, err := runLatency(context.Background(), 20*time.Millisecond, SystemInfo{})
	if err != nil {
		t.Fatal(err)
	}
	if r.Value <= 0 || r.HigherIsBetter {
		t.Errorf("latency result bad (lower is better): %+v", r)
	}
}

func TestRunStorage(t *testing.T) {
	dir := t.TempDir()
	r, err := runStorage(SystemInfo{}, dir, 4*1024*1024)
	if err != nil {
		t.Fatal(err)
	}
	if r.Value <= 0 {
		t.Errorf("storage result bad: %+v", r)
	}
	left, _ := filepath.Glob(filepath.Join(dir, "kairo-bench-*"))
	if len(left) != 0 {
		t.Errorf("benchmark temp file not cleaned: %v", left)
	}
}

func TestRunNetworkLoopback(t *testing.T) {
	r, err := runNetwork(SystemInfo{}, 2*1024*1024)
	if err != nil {
		t.Skipf("loopback unavailable: %v", err)
	}
	if r.Value <= 0 {
		t.Errorf("network result bad: %+v", r)
	}
	if len(r.Warnings) == 0 {
		t.Error("network benchmark should carry a path caveat")
	}
}

func TestVirtualizedWarning(t *testing.T) {
	r := makeResult("cpu", 1, "MB/s", true, time.Second, SystemInfo{Virtualized: true, CPUGovernor: "schedutil"}, nil)
	found := map[string]bool{}
	for _, w := range r.Warnings {
		found[w] = true
	}
	if !found["system is virtualized; results may be influenced by the host and noisy neighbors"] {
		t.Error("virtualization warning missing")
	}
	if !found["CPU frequency scaling active (governor schedutil); results may vary between runs"] {
		t.Error("frequency scaling warning missing")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bench.json")
	rs := []Result{{Category: "cpu", Value: 100, Unit: "MB/s"}, {Category: "memory", Value: 2, Unit: "GB/s"}}
	if err := Save(path, rs); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Category != "cpu" {
		t.Errorf("round trip mismatch: %+v", got)
	}
}

func TestLoadRejectsForeignFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "junk.json")
	os.WriteFile(path, []byte(`{"not":"kairo"}`), 0o600)
	if _, err := Load(path); err == nil {
		t.Fatal("foreign JSON must be rejected")
	}
}

func TestCompareDeliversDeltas(t *testing.T) {
	before := []Result{{Category: "cpu", Value: 100, Unit: "MB/s", HigherIsBetter: true}}
	after := []Result{{Category: "cpu", Value: 150, Unit: "MB/s", HigherIsBetter: true}}
	diffs := Compare(before, after)
	if len(diffs) != 1 {
		t.Fatalf("want 1 diff, got %d", len(diffs))
	}
	d := diffs[0]
	if d.DeltaPct != 50 {
		t.Errorf("DeltaPct=%v want=50", d.DeltaPct)
	}
	if !d.Improved {
		t.Error("cpu +50%% must be an improvement")
	}
}

func TestCompareMissingCategoryTolerated(t *testing.T) {
	before := []Result{{Category: "cpu", Value: 1, Unit: "MB/s"}}
	after := []Result{{Category: "memory", Value: 2, Unit: "GB/s"}}
	if diffs := Compare(before, after); len(diffs) != 0 {
		t.Errorf("no shared category should mean no diffs, got %v", diffs)
	}
}

func TestCompareFiles(t *testing.T) {
	dir := t.TempDir()
	b := filepath.Join(dir, "b.json")
	a := filepath.Join(dir, "a.json")
	Save(b, []Result{{Category: "storage", Value: 500, Unit: "MB/s (read)", HigherIsBetter: true}})
	Save(a, []Result{{Category: "storage", Value: 550, Unit: "MB/s (read)", HigherIsBetter: true}})
	diffs, err := CompareFiles(b, a)
	if err != nil {
		t.Fatal(err)
	}
	if diffs[0].DeltaPct != 10 {
		t.Errorf("delta want=10 got=%v", diffs[0].DeltaPct)
	}
}

func TestJSONMatchesSchema(t *testing.T) {
	r := Result{Category: "latency", Value: 42, Unit: "ns/op", HigherIsBetter: false, System: SystemInfo{Kernel: "6.1"}}
	data, _ := json.Marshal(r)
	var out struct {
		Category       string `json:"category"`
		HigherIsBetter bool   `json:"higher_is_better"`
		System         struct {
			Kernel string `json:"kernel"`
		} `json:"system"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Category != "latency" || !(out.HigherIsBetter == false) || out.System.Kernel != "6.1" {
		t.Errorf("schema drift: %+v", out)
	}
}
