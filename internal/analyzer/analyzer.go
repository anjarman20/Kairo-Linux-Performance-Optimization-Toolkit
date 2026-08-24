// Package analyzer defines the base contracts for all Kairo analyzers.
//
// Analyzers are strictly read-only. They answer one question: what is the
// current state of this subsystem? Optimization lives in a separate package
// so detection can never mutate the system.
package analyzer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Status characterizes a capability or a whole category.
type Status string

const (
	StatusOk   Status = "ok"
	StatusWarn Status = "warning"
	StatusSkip Status = "unsupported"
)

// Capability is one detected attribute of a subsystem. Weight is the maximum
// number of score points this capability can earn; it counts only when the
// feature is actually present and healthy, so unsupported hardware never
// inflates a score.
type Capability struct {
	Name   string  `json:"name"`
	Value  string  `json:"value"`
	Weight float64 `json:"weight"`
	Status Status  `json:"status"`
}

// Finding is a human-actionable observation, distinct from raw capability data.
type Finding struct {
	Severity string `json:"severity"`
	Category string `json:"category"`
	Message  string `json:"message"`
}

// Result is the structured output of a single analyzer run.
type Result struct {
	Name         string       `json:"name"`
	Status       Status       `json:"status"`
	Score        float64      `json:"score"`
	MaxScore     float64      `json:"max_score"`
	Scored       bool         `json:"-"`
	Summary      string       `json:"summary"`
	Findings     []Finding    `json:"findings"`
	Capabilities []Capability `json:"capabilities"`
}

// Analyzer is implemented by every subsystem module. Detect must not modify
// the system and must degrade gracefully on unsupported hardware/kernels.
type Analyzer interface {
	Name() string
	Detect(ctx context.Context) (Result, error)
}

// ScoreCaps reduces capabilities to achieved and maximum points using the
// documented rule: full weight on ok, half on warning, zero on unsupported.
func ScoreCaps(caps []Capability) (got, max float64) {
	for _, c := range caps {
		max += c.Weight
		switch c.Status {
		case StatusOk:
			got += c.Weight
		case StatusWarn:
			got += c.Weight / 2
		}
	}
	return got, max
}

// Finalize computes score, status, summary and findings from raw capabilities.
func Finalize(name string, caps []Capability, scored bool) Result {
	got, max := ScoreCaps(caps)
	st := StatusOk
	if got < max {
		st = StatusWarn
	}
	okCount := 0
	for _, c := range caps {
		if c.Status == StatusOk {
			okCount++
		}
	}
	res := Result{
		Name:         name,
		Status:       st,
		Score:        got,
		MaxScore:     max,
		Scored:       scored,
		Summary:      fmt.Sprintf("%d/%d capabilities optimal", okCount, len(caps)),
		Capabilities: caps,
	}
	for _, c := range caps {
		switch c.Status {
		case StatusWarn:
			res.Findings = append(res.Findings, Finding{"warning", name, fmt.Sprintf("%s: %s (needs review)", c.Name, c.Value)})
		case StatusSkip:
			res.Findings = append(res.Findings, Finding{"info", name, fmt.Sprintf("%s: feature unsupported", c.Name)})
		}
	}
	return res
}

// ReadTrim returns a trimmed single-line file read, the standard way to read
// sysctl-style /proc and /sys pseudo-files.
func ReadTrim(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// PathExists reports whether a path exists without escalating errors.
func PathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// Globs returns sorted matches for a pattern (e.g. /sys/class/net/*).
func Globs(pattern string) []string {
	m, _ := filepath.Glob(pattern)
	return m
}

// Run executes an external command with a hard timeout. Used only where no
// structured kernel interface exists (tc, ethtool). Args are passed verbatim,
// never interpolated through a shell.
func Run(ctx context.Context, timeout time.Duration, name string, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, name, args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
