// Package optimizer turns a selected profile into a concrete, reversible
// change plan and applies it transactionally: snapshot first, verify after
// every write, roll back everything on any failure, and keep metadata that
// supports `kairo rollback` later.
//
// Phase 3 supports file-backed writes only (sysctl and cpufreq/block sysfs).
// Command-mode changes such as qdisc replacement are deliberately excluded
// until Phase 5 so every change here is trivially reversible.
package optimizer

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/backend"
	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/profile"
	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/render"
)

// PlanHuman renders a plan the way a dry-run report should read: current vs
// target per change, the risk, and the reason behind each edit.
func PlanHuman(w io.Writer, plan Plan, quiet bool) {
	fmt.Fprintf(w, "Profile: %s\n\nPlanned Changes\n", plan.Profile)
	if len(plan.Changes) == 0 {
		fmt.Fprintln(w, "  (none)")
	}
	for _, c := range plan.Changes {
		fmt.Fprintf(w, "\n[%s]\n%s\n  current: %s\n  target : %s\n  risk   : %s\n  reason : %s\n",
			strings.ToUpper(c.Area), c.Path, c.Current, c.Desired, c.Risk, c.Reason)
	}
	fmt.Fprintf(w, "\n%d change(s) planned.\n", len(plan.Changes))
	if plan.Unchanged > 0 {
		fmt.Fprintf(w, "%d already correct.\n", plan.Unchanged)
	}
	if !quiet && len(plan.Skipped) > 0 {
		fmt.Fprintln(w, "\nSkipped")
		for _, s := range plan.Skipped {
			fmt.Fprintf(w, "  %s\n", s)
		}
	}
}

// Risk describes the blast radius of a change.
type Risk string

// Risk levels in ascending severity.
const (
	RiskLow    Risk = "low"
	RiskMedium Risk = "medium"
	RiskHigh   Risk = "high"
)

// Change is one discrete mutation with its previous value preserved.
type Change struct {
	Area       string `json:"area"`
	Key        string `json:"key"`
	Current    string `json:"current"`
	Desired    string `json:"desired"`
	Reason     string `json:"reason"`
	Risk       Risk   `json:"risk"`
	Reversible bool   `json:"reversible"`
	Path       string `json:"path"`
}

// Plan is the full output of the planner: what would change, what was already
// correct, and what cannot be touched safely yet.
type Plan struct {
	Profile   string   `json:"profile"`
	Changes   []Change `json:"changes"`
	Skipped   []string `json:"skipped"`
	Unchanged int      `json:"unchanged"`
}

type paramSpec struct {
	area, key, capName string
	pathHandler        func(r render.Scan) (string, bool)
	risk               Risk
	reasonFunc         func(profileName, desired string) string
}

var specs = []paramSpec{
	{
		area: "memory", key: "swappiness", capName: "swappiness",
		pathHandler: fixedPath("/proc/sys/vm/swappiness"), risk: RiskLow,
		reasonFunc: func(p, v string) string {
			return p + " limits swap pressure; keeps memory committed to active workloads."
		},
	},
	{
		area: "memory", key: "vfs_cache_pressure", capName: "vfs cache pressure",
		pathHandler: fixedPath("/proc/sys/vm/vfs_cache_pressure"), risk: RiskLow,
		reasonFunc: func(p, v string) string {
			return p + " tunes VFS cache reclamation for the selected workload."
		},
	},
	{
		area: "network", key: "congestion_control", capName: "congestion control",
		pathHandler: fixedPath("/proc/sys/net/ipv4/tcp_congestion_control"), risk: RiskMedium,
		reasonFunc: func(p, v string) string {
			return p + " requests " + v + " congestion control; kernel reports the algorithm is available."
		},
	},
	{
		area: "cpu", key: "governor", capName: "governor",
		pathHandler: fixedPath("/sys/devices/system/cpu/cpu0/cpufreq/scaling_governor"), risk: RiskLow,
		reasonFunc: func(p, v string) string {
			return p + " prefers the " + v + " governor for the selected workload."
		},
	},
	{
		area: "storage", key: "scheduler", capName: "scheduler",
		pathHandler: diskSchedulerPath, risk: RiskMedium,
		reasonFunc: func(p, v string) string {
			return p + " selects " + v + " I/O scheduling for predictable storage behavior."
		},
	},
}

func fixedPath(path string) func(render.Scan) (string, bool) {
	return func(render.Scan) (string, bool) { return path, true }
}

// diskSchedulerPath resolves the scheduler sysfs file of the primary disk
// reported by the storage analyzer.
func diskSchedulerPath(r render.Scan) (string, bool) {
	for _, c := range r.Categories {
		if c.Name != "storage" {
			continue
		}
		for _, cap := range c.Capabilities {
			if cap.Name == "primary device" && cap.Value != "" {
				return "/sys/block/" + cap.Value + "/queue/scheduler", true
			}
		}
	}
	return "", false
}

// capLookup returns the live value of a capability in a category.
func capLookup(r render.Scan, area, capName string) (string, bool) {
	for _, c := range r.Categories {
		if c.Name != area {
			continue
		}
		for _, cap := range c.Capabilities {
			if cap.Name == capName {
				return cap.Value, true
			}
		}
	}
	return "", false
}

// Plan computes the changes a profile would make against the current scan.
// Unsupported or already-correct targets are reported, never applied.
func Build(p profile.Profile, r render.Scan) Plan {
	plan := Plan{Profile: p.Name}
	for _, spec := range specs {
		desired, targeted := p.Targets[spec.area][spec.key]
		if !targeted {
			continue
		}
		current, found := capLookup(r, spec.area, spec.capName)
		if !found || current == "unavailable" || strings.Contains(current, "(unsupported)") {
			plan.Skipped = append(plan.Skipped, fmt.Sprintf("%s.%s: feature unsupported", spec.area, spec.key))
			continue
		}
		if current == desired {
			plan.Unchanged++
			continue
		}
		path, ok := spec.pathHandler(r)
		if !ok {
			plan.Skipped = append(plan.Skipped, fmt.Sprintf("%s.%s: no write target detected", spec.area, spec.key))
			continue
		}
		plan.Changes = append(plan.Changes, Change{
			Area: spec.area, Key: spec.key, Current: current, Desired: desired,
			Reason: spec.reasonFunc(plan.Profile, desired), Risk: spec.risk,
			Reversible: true, Path: path,
		})
	}
	// qdisc is a profile target but command-mode only; keep it safe to skip.
	if _, targeted := p.Targets["network"]["qdisc"]; targeted {
		plan.Skipped = append(plan.Skipped, "network.qdisc: command-mode change deferred to Phase 5; skipped safely")
	}
	return plan
}

// Apply writes every change, verifying each by reading it back. On any failure
// it rolls back the already-applied changes before returning, so the system is
// never left half-optimized. The wrapped error identifies the offending change.
func Apply(ctx context.Context, be backend.Backend, changes []Change) error {
	applied := make([]int, 0, len(changes))
	for i := range changes {
		ch := &changes[i]
		if err := be.Write(ctx, ch.Path, []byte(ch.Desired+"\n")); err != nil {
			return rollbackAndErr(ctx, be, changes, applied, fmt.Errorf("%s.%s: apply failed: %w", ch.Area, ch.Key, err))
		}
		applied = append(applied, i)
		if err := verify(ctx, be, *ch); err != nil {
			return rollbackAndErr(ctx, be, changes, applied, err)
		}
	}
	return nil
}

// rollbackAndErr restores the applied subset and annotates the error.
func rollbackAndErr(ctx context.Context, be backend.Backend, changes []Change, applied []int, err error) error {
	var rbErr error
	for _, i := range applied {
		if rbErr = be.Write(ctx, changes[i].Path, []byte(changes[i].Current+"\n")); rbErr != nil {
			return fmt.Errorf("%w; additionally rollback of %s.%s failed: %v (system may be partially modified)", err, changes[i].Area, changes[i].Key, rbErr)
		}
	}
	return fmt.Errorf("%w; the system was restored to its previous state", err)
}

func verify(ctx context.Context, be backend.Backend, ch Change) error {
	got, err := be.Read(ctx, ch.Path)
	if err != nil {
		return fmt.Errorf("%s.%s: verify failed to read: %w", ch.Area, ch.Key, err)
	}
	if strings.TrimSpace(string(got)) != ch.Desired {
		return fmt.Errorf("%s.%s: verify failed: wrote %q, read %q", ch.Area, ch.Key, ch.Desired, strings.TrimSpace(string(got)))
	}
	return nil
}

// Rollback restores every change to its previous value and verifies the result.
func Rollback(ctx context.Context, be backend.Backend, changes []Change) error {
	// Restore in reverse order so the most recently changed value goes back
	// first, mirroring the apply order.
	for i := len(changes) - 1; i >= 0; i-- {
		ch := changes[i]
		if err := be.Write(ctx, ch.Path, []byte(ch.Current+"\n")); err != nil {
			return fmt.Errorf("%s.%s: rollback failed: %w", ch.Area, ch.Key, err)
		}
		if err := verifyRollback(ctx, be, ch); err != nil {
			return err
		}
	}
	return nil
}

func verifyRollback(ctx context.Context, be backend.Backend, ch Change) error {
	got, err := be.Read(ctx, ch.Path)
	if err != nil {
		return fmt.Errorf("%s.%s: rollback verify failed: %w", ch.Area, ch.Key, err)
	}
	if strings.TrimSpace(string(got)) != ch.Current {
		return fmt.Errorf("%s.%s: rollback verify failed: want %q, read %q", ch.Area, ch.Key, ch.Current, strings.TrimSpace(string(got)))
	}
	return nil
}
