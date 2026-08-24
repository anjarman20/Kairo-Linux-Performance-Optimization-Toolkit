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
	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/render"
)

type scaner struct {
	analyzers []analyzer.Analyzer
}

// Scan runs the full detection pipeline. A failing analyzer degrades into an
// informational category instead of aborting the whole scan.
func Scan(ctx context.Context, version string) render.Scan {
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
	return render.Scan{System: sys, Categories: cats, Score: score, MaxScore: maxScore, Findings: findings}
}

func capVal(res analyzer.Result, name string) string {
	for _, c := range res.Capabilities {
		if c.Name == name {
			return c.Value
		}
	}
	return ""
}
