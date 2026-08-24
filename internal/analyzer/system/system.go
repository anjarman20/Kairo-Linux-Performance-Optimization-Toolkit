// Package system reports host identity and privilege state. Informational.
package system

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/analyzer"
)

// Analyzer implements analyzer.Analyzer for host-level facts.
type Analyzer struct{}

// Name implements analyzer.Analyzer.
func (Analyzer) Name() string { return "system" }

func distro() string {
	raw, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "unknown"
	}
	for _, ln := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(ln, "PRETTY_NAME=") {
			return strings.Trim(strings.TrimPrefix(ln, "PRETTY_NAME="), "\"")
		}
	}
	return "unknown"
}

func uptime() string {
	raw, err := analyzer.ReadTrim("/proc/uptime")
	if err != nil {
		return "unknown"
	}
	sec, err := strconv.ParseFloat(strings.Fields(raw)[0], 64)
	if err != nil {
		return "unknown"
	}
	d := int(sec) / 86400
	return strconv.Itoa(d) + " day(s)"
}

// Detect implements analyzer.Analyzer.
func (Analyzer) Detect(ctx context.Context) (analyzer.Result, error) {
	host, err := os.Hostname()
	if err != nil {
		host = "unknown"
	}
	root := "yes"
	if os.Geteuid() != 0 {
		root = "no"
	}
	caps := []analyzer.Capability{
		{Name: "hostname", Value: host, Weight: 0, Status: analyzer.StatusOk},
		{Name: "distro", Value: distro(), Weight: 0, Status: analyzer.StatusOk},
		{Name: "uptime", Value: uptime(), Weight: 0, Status: analyzer.StatusOk},
		{Name: "root ready", Value: root, Weight: 0, Status: analyzer.StatusOk},
		{Name: "timestamp", Value: time.Now().UTC().Format(time.RFC3339), Weight: 0, Status: analyzer.StatusOk},
	}
	return analyzer.Finalize("system", caps, false), nil
}
