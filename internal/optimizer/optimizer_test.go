package optimizer

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/analyzer"
	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/backend"
	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/profile"
	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/render"
)

// fake is an in-memory backend that can be told to fail one specific path.
type fake struct {
	files  map[string]string
	failOn string
	writes []string
}

func (f *fake) Read(_ context.Context, path string) ([]byte, error) {
	v, ok := f.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return []byte(v), nil
}

func (f *fake) Write(_ context.Context, path string, data []byte) error {
	f.writes = append(f.writes, path)
	if path == f.failOn {
		return errors.New("permission denied")
	}
	f.files[path] = string(data)
	return nil
}

func newFake() *fake {
	return &fake{files: map[string]string{}}
}

// testScan builds a representative analyze scan with editable values.
func testScan(mut func(m map[string]string)) render.Scan {
	vals := map[string]string{
		"swappiness":         "60",
		"vfs cache pressure": "100",
		"congestion control": "cubic",
		"governor":           "schedutil",
		"scheduler":          "none",
		"primary device":     "sda",
		"qdisc":              "mq",
	}
	if mut != nil {
		mut(vals)
	}
	cat := func(name string, keys ...string) render.Category {
		var caps []analyzer.Capability
		for _, k := range keys {
			if v, ok := vals[k]; ok {
				caps = append(caps, analyzer.Capability{Name: k, Value: v, Status: analyzer.StatusOk})
			}
		}
		return render.Category{Name: name, Capabilities: caps}
	}
	return render.Scan{Categories: []render.Category{
		cat("memory", "swappiness", "vfs cache pressure"),
		cat("cpu", "governor"),
		cat("network", "congestion control", "qdisc"),
		cat("storage", "primary device", "scheduler"),
	}}
}

func gaming() profile.Profile {
	return profile.Profile{
		Name: "gaming",
		Targets: map[string]profile.Target{
			"cpu":     {"governor": "performance"},
			"network": {"congestion_control": "bbr", "qdisc": "fq"},
		},
	}
}

func TestPlanChanges(t *testing.T) {
	plan := Build(gaming(), testScan(nil))
	if len(plan.Changes) != 2 {
		t.Fatalf("want 2 changes, got %d: %+v", len(plan.Changes), plan.Changes)
	}
	seen := map[string]string{}
	for _, c := range plan.Changes {
		seen[c.Area+"."+c.Key] = c.Desired
		if !c.Reversible {
			t.Errorf("%s.%s must be reversible", c.Area, c.Key)
		}
	}
	if seen["cpu.governor"] != "performance" || seen["network.congestion_control"] != "bbr" {
		t.Errorf("unexpected targets: %v", seen)
	}
	hasSkip := false
	for _, s := range plan.Skipped {
		if strings.Contains(s, "network.qdisc") {
			hasSkip = true
		}
	}
	if !hasSkip {
		t.Error("qdisc target must be reported as skipped in Phase 3")
	}
}

func TestPlanUnchangedNotListed(t *testing.T) {
	plan := Build(gaming(), testScan(func(m map[string]string) {
		m["governor"] = "performance"
	}))
	if len(plan.Changes) != 1 {
		t.Fatalf("governor already correct should not be a change; got %d", len(plan.Changes))
	}
	if plan.Unchanged != 1 {
		t.Errorf("Unchanged=%d want=1", plan.Unchanged)
	}
}

func TestPlanUnsupportedSkipped(t *testing.T) {
	plan := Build(gaming(), testScan(func(m map[string]string) {
		delete(m, "governor")
	}))
	skip := false
	for _, s := range plan.Skipped {
		if strings.Contains(s, "cpu.governor") {
			skip = true
		}
	}
	if !skip {
		t.Error("missing governor capability must produce a skipped entry, not a change")
	}
}

func TestApplyVerifiesAndWrites(t *testing.T) {
	f := newFake()
	changes := []Change{
		{Area: "cpu", Key: "governor", Current: "schedutil", Desired: "performance", Path: "/proc/x/gov"},
		{Area: "memory", Key: "swappiness", Current: "60", Desired: "10", Path: "/proc/x/swap"},
	}
	if err := Apply(context.Background(), f, changes); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if f.files["/proc/x/gov"] != "performance\n" || f.files["/proc/x/swap"] != "10\n" {
		t.Errorf("backend not updated: %v", f.files)
	}
}

func TestApplyFailureRollsBack(t *testing.T) {
	f := newFake()
	f.failOn = "/proc/x/swap"
	changes := []Change{
		{Area: "cpu", Key: "governor", Current: "schedutil", Desired: "performance", Path: "/proc/x/gov"},
		{Area: "memory", Key: "swappiness", Current: "60", Desired: "10", Path: "/proc/x/swap"},
	}
	err := Apply(context.Background(), f, changes)
	if err == nil {
		t.Fatal("expected apply error")
	}
	if !strings.Contains(err.Error(), "restored to its previous state") {
		t.Errorf("error must state rollback: %v", err)
	}
	if got := f.files["/proc/x/gov"]; got != "schedutil\n" {
		t.Errorf("governor not rolled back, got %q", got)
	}
	if _, ok := f.files["/proc/x/swap"]; ok {
		t.Error("failed change must not be written")
	}
}

func TestRollbackRestoresPreviousValues(t *testing.T) {
	f := newFake()
	changes := []Change{
		{Area: "cpu", Key: "governor", Current: "schedutil", Desired: "performance", Path: "/proc/x/gov"},
		{Area: "memory", Key: "swappiness", Current: "60", Desired: "10", Path: "/proc/x/swap"},
	}
	if err := Apply(context.Background(), f, changes); err != nil {
		t.Fatal(err)
	}
	if err := Rollback(context.Background(), f, changes); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if f.files["/proc/x/gov"] != "schedutil\n" || f.files["/proc/x/swap"] != "60\n" {
		t.Errorf("rollback restored wrong values: %v", f.files)
	}
}

func TestRollbackOnlyTouchesItsChanges(t *testing.T) {
	f := newFake()
	f.files["/etc/important.conf"] = "admin value"
	changes := []Change{{Area: "cpu", Key: "governor", Current: "schedutil", Desired: "performance", Path: "/proc/x/gov"}}
	if err := Apply(context.Background(), f, changes); err != nil {
		t.Fatal(err)
	}
	if err := Rollback(context.Background(), f, changes); err != nil {
		t.Fatal(err)
	}
	if f.files["/etc/important.conf"] != "admin value" {
		t.Error("rollback must never touch unrelated paths")
	}
}

func TestSnapshotRoundTrip(t *testing.T) {
	base := t.TempDir()
	id := NewID(time.Now())
	m := Metadata{ID: id, Profile: "database", State: StateSnapshot,
		Changes: []Change{{Area: "memory", Key: "swappiness", Current: "60", Desired: "10", Path: "/proc/sys/vm/swappiness"}}}
	if err := SaveSnapshot(base, m); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSnapshot(base, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Changes) != 1 || got.Changes[0].Desired != "10" {
		t.Errorf("snapshot mismatch: %+v", got)
	}
	if lat, _ := Latest(base); lat != id {
		t.Errorf("Latest=%q want=%q", lat, id)
	}
	if err := SetState(base, id, StateCommitted); err != nil {
		t.Fatal(err)
	}
	if m2, _ := LoadSnapshot(base, id); m2.State != StateCommitted {
		t.Errorf("state not persisted: %q", m2.State)
	}
}

func TestLatestEmpty(t *testing.T) {
	if id, _ := Latest(t.TempDir()); id != "" {
		t.Errorf("Latest on empty base=%q", id)
	}
}

var _ backend.Backend = (*fake)(nil)
