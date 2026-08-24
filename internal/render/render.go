// Package render formats analyzer results for humans and stable JSON.
// JSON mode never mixes in human text.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/analyzer"
)

// System carries host identity for both output modes.
type System struct {
	Version      string `json:"version"`
	Hostname     string `json:"hostname"`
	Kernel       string `json:"kernel"`
	Architecture string `json:"architecture"`
	Distro       string `json:"distro"`
	Root         bool   `json:"root"`
	Timestamp    string `json:"timestamp"`
}

// Category is one analyzer's rendered result.
type Category struct {
	Name         string                `json:"name"`
	Status       analyzer.Status       `json:"status"`
	Score        float64               `json:"score"`
	MaxScore     float64               `json:"max_score"`
	Summary      string                `json:"summary"`
	Capabilities []analyzer.Capability `json:"capabilities"`
}

// Hint compares one profile target against the live analyzed value.
type Hint struct {
	Area    string `json:"area"`
	Key     string `json:"key"`
	Current string `json:"current"`
	Target  string `json:"target"`
	Status  string `json:"status"` // match | differs | unsupported
}

// Profile carries the actively selected workload profile in a scan.
type Profile struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Hints       []Hint `json:"hints"`
}

// Scan is the full machine-readable report.
type Scan struct {
	System     System             `json:"system"`
	Categories []Category         `json:"categories"`
	Score      float64            `json:"score"`
	MaxScore   float64            `json:"max_score"`
	Findings   []analyzer.Finding `json:"findings"`
	Profile    *Profile           `json:"profile,omitempty"`
}

// JSON writes the complete scan as indented, stable JSON.
func JSON(w io.Writer, s Scan) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(s)
}

// Human writes the terminal report.
func Human(w io.Writer, s Scan, quiet, verbose bool) {
	fmt.Fprintf(w, "Kairo System Analyzer (v%s)\n\n", s.System.Version)
	for _, c := range s.Categories {
		if c.MaxScore == 0 {
			continue // informational categories rendered later
		}
		fmt.Fprintf(w, "%-8s %4.0f/%-4.0f  %s\n", strings.ToUpper(c.Name), c.Score, c.MaxScore, c.Summary)
	}
	fmt.Fprintf(w, "\nTotal: %.0f/%.0f\n", s.Score, s.MaxScore)

	if !quiet {
		fmt.Fprintln(w)
		for _, c := range s.Categories {
			fmt.Fprintln(w, strings.ToUpper(c.Name))
			for _, cap := range c.Capabilities {
				marker := ""
				switch cap.Status {
				case analyzer.StatusWarn:
					marker = " (!)"
				case analyzer.StatusSkip:
					marker = " (unsupported)"
				}
				fmt.Fprintf(w, "  %-20s: %s%s\n", cap.Name, cap.Value, marker)
			}
			fmt.Fprintln(w)
		}
	}

	if s.Profile != nil {
		fmt.Fprintf(w, "Profile: %s\n  %s\n\nProfile targets vs current\n", s.Profile.Name, s.Profile.Description)
		for _, h := range s.Profile.Hints {
			fmt.Fprintf(w, "  %-34s target=%s current=%s [%s]\n", h.Area+"."+h.Key, h.Target, h.Current, h.Status)
		}
		fmt.Fprintln(w)
	}

	if len(s.Findings) > 0 {
		fmt.Fprintln(w, "Findings")
		for _, f := range s.Findings {
			fmt.Fprintf(w, "  [%s] %s: %s\n", f.Severity, f.Category, f.Message)
		}
	}
	if verbose {
		fmt.Fprintf(w, "\nUsage: root=%t, config: see --help\n", s.System.Root)
	}
}
