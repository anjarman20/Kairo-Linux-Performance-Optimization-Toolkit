// Package kairo orchestrates a full system scan: runs every analyzer and
// aggregates results into a single report. Read-only.
package kairo

import (
	"context"
	"os"
	"runtime"

	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/analyzer"
	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/analyzer/cpu"
	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/analyzer/kernel"
	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/analyzer/memory"
	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/analyzer/network"
	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/analyzer/storage"
	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/analyzer/system"
	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/profile"
	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/render"
)

// Scan runs the full detection pipeline. A failing analyzer degrades into an
// informational category instead of aborting the whole scan. A non-nil profile
// adds profile-aware hints comparing targets to live values.
func Scan(ctx context.Context, version string, p *profile.Profile) render.Scan {
	as := []analyzer.Analyzer{
		system.Analyzer{},
		kernel.Analyzer{},
		cpu.Analyzer{},
		memory.Analyzer{},
		network.Analyzer{},
		storage.Analyzer{},
	}

	var score, maxScore float64
	var cats []render.Category
	var findings []analyzer.Finding
	meta := map[string]analyzer.Result{}

	for _, a := range as {
		res, err := a.Detect(ctx)
		if err != nil {
			res = analyzer.Result{Name: a.Name(), Summary: "detection failed: " + err.Error(), Status: analyzer.StatusSkip}
		}
		meta[a.Name()] = res
		if res.Scored {
			score += res.Score
			maxScore += res.MaxScore
		}
		findings = append(findings, res.Findings...)
		cats = append(cats, render.Category{
			Name:         res.Name,
			Status:       res.Status,
			Score:        res.Score,
			MaxScore:     res.MaxScore,
			Summary:      res.Summary,
			Capabilities: res.Capabilities,
		})
	}

	host, _ := os.Hostname()
	sys := render.System{
		Version:      version,
		Hostname:     host,
		Kernel:       capVal(meta["kernel"], "kernel"),
		Architecture: runtime.GOARCH,
		Distro:       capVal(meta["system"], "distro"),
		Root:         os.Geteuid() == 0,
		Timestamp:    capVal(meta["system"], "timestamp"),
	}
	sc := render.Scan{System: sys, Categories: cats, Score: score, MaxScore: maxScore, Findings: findings}
	if p != nil {
		sc.Profile = &render.Profile{Name: p.Name, Description: p.Description, Hints: hintsFor(meta, p)}
	}
	return sc
}

// targetToCap maps a profile parameter name to the capability name emitted by
// the matching analyzer, so hints reuse already-detected values.
var targetToCap = map[string]string{
	"governor":           "governor",
	"congestion_control": "congestion control",
	"qdisc":              "qdisc",
	"swappiness":         "swappiness",
	"vfs_cache_pressure": "vfs cache pressure",
	"scheduler":          "scheduler",
}

func hintsFor(meta map[string]analyzer.Result, p *profile.Profile) []render.Hint {
	var hints []render.Hint
	for area, params := range p.Targets {
		res, ok := meta[area]
		if !ok {
			continue
		}
		// Build capability lookup by name for this area.
		byName := map[string]analyzer.Capability{}
		for _, c := range res.Capabilities {
			byName[c.Name] = c
		}
		for key, target := range params {
			capName, known := targetToCap[key]
			if !known {
				continue
			}
			hint := render.Hint{Area: area, Key: key, Target: target, Status: "differs"}
			cap, found := byName[capName]
			switch {
			case !found || cap.Status == analyzer.StatusSkip:
				hint.Current = "unsupported"
				hint.Status = "unsupported"
			case target == cap.Value:
				hint.Current = cap.Value
				hint.Status = "match"
			default:
				hint.Current = cap.Value
			}
			hints = append(hints, hint)
		}
	}
	return hints
}

func capVal(res analyzer.Result, name string) string {
	for _, c := range res.Capabilities {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}
