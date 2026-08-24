// Package storage analyzes block devices, I/O scheduler and queue parameters.
// Filesystem mount options and device writes are out of scope here.
package storage

import (
	"context"
	"strconv"
	"strings"

	"github.com/anjarman20/Kairo-Linux-Performance-Optimization-Toolkit/internal/analyzer"
)

// Analyzer implements analyzer.Analyzer for the storage subsystem.
type Analyzer struct{}

// Name implements analyzer.Analyzer.
func (Analyzer) Name() string { return "storage" }

// realDisks returns block devices that are not virtual/loop/cdrom.
// realDisks returns whole block devices that are not virtual/loop/cdrom or
// partitions. Partitions carry a "partition" attribute file in their dir.
func realDisks() []string {
	var disks []string
	for _, p := range analyzer.Globs("/sys/class/block/*") {
		name := strings.TrimPrefix(p, "/sys/class/block/")
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") || strings.HasPrefix(name, "zd") {
			continue
		}
		if strings.HasPrefix(name, "sr") || strings.HasPrefix(name, "dm-") {
			continue
		}
		if analyzer.PathExists(p + "/partition") {
			continue
		}
		disks = append(disks, name)
	}
	return disks
}

func currentScheduler(disk string) (string, bool) {
	v, err := analyzer.ReadTrim("/sys/class/block/" + disk + "/queue/scheduler")
	if err != nil {
		return "", false
	}
	// Format is "[mq-deadline] none"; brackets mark the active scheduler.
	for _, tok := range strings.Fields(v) {
		if len(tok) > 2 && tok[0] == '[' && tok[len(tok)-1] == ']' {
			return strings.Trim(tok, "[]"), true
		}
	}
	fields := strings.Fields(v)
	if len(fields) > 0 {
		return fields[0], true
	}
	return v, true
}

func diskModel(disk string) string {
	model, err := analyzer.ReadTrim("/sys/class/block/" + disk + "/device/model")
	if err == nil && model != "" {
		return model
	}
	// NVMe models live one level deeper under the transport device.
	vendor, err1 := analyzer.ReadTrim("/sys/class/block/" + disk + "/device/vendor")
	if err1 == nil && vendor != "" && vendor != "0x0" {
		return vendor + " " + model
	}
	return ""
}

// Detect implements analyzer.Analyzer.
func (Analyzer) Detect(ctx context.Context) (analyzer.Result, error) {
	disks := realDisks()
	if len(disks) == 0 {
		caps := []analyzer.Capability{{Name: "block devices", Value: "none", Weight: 25, Status: analyzer.StatusSkip}}
		return analyzer.Finalize("storage", caps, true), nil
	}

	caps := []analyzer.Capability{
		{Name: "block devices", Value: strconv.Itoa(len(disks)) + " (" + strings.Join(disks, ", ") + ")", Weight: 2, Status: analyzer.StatusOk},
	}
	primary := disks[0]
	caps = append(caps,
		analyzer.Capability{Name: "primary device", Value: primary, Weight: 3, Status: analyzer.StatusOk},
		diskType(primary),
		schedCap(primary),
		readAheadCap(primary),
		queueDepthCap(primary),
	)

	model := diskModel(primary)
	if model != "" {
		caps = append(caps, analyzer.Capability{Name: "model", Value: model, Weight: 4, Status: analyzer.StatusOk})
	} else {
		caps = append(caps, analyzer.Capability{Name: "model", Value: "unknown", Weight: 4, Status: analyzer.StatusSkip})
	}
	return analyzer.Finalize("storage", caps, true), nil
}

func diskType(disk string) analyzer.Capability {
	if strings.HasPrefix(disk, "nvme") {
		return analyzer.Capability{Name: "type", Value: "nvme", Weight: 4, Status: analyzer.StatusOk}
	}
	rot, err := analyzer.ReadTrim("/sys/class/block/" + disk + "/queue/rotational")
	if err != nil {
		return analyzer.Capability{Name: "type", Value: "unknown", Weight: 4, Status: analyzer.StatusSkip}
	}
	t := "ssd"
	if rot == "1" {
		t = "hdd"
	}
	return analyzer.Capability{Name: "type", Value: t, Weight: 4, Status: analyzer.StatusOk}
}

func schedCap(disk string) analyzer.Capability {
	sched, ok := currentScheduler(disk)
	if !ok {
		return analyzer.Capability{Name: "scheduler", Value: "none", Weight: 5, Status: analyzer.StatusSkip}
	}
	st := analyzer.StatusOk
	if sched == "none" && !strings.HasPrefix(disk, "nvme") {
		// mq-deadline preferred on HDD/SSD with a scheduler available.
		st = analyzer.StatusWarn
	}
	return analyzer.Capability{Name: "scheduler", Value: sched, Weight: 5, Status: st}
}

func readAheadCap(disk string) analyzer.Capability {
	v, err := analyzer.ReadTrim("/sys/class/block/" + disk + "/queue/read_ahead_kb")
	if err != nil {
		return analyzer.Capability{Name: "read-ahead", Value: "unavailable", Weight: 4, Status: analyzer.StatusSkip}
	}
	return analyzer.Capability{Name: "read-ahead", Value: v + " KB", Weight: 4, Status: analyzer.StatusOk}
}

func queueDepthCap(disk string) analyzer.Capability {
	v, err := analyzer.ReadTrim("/sys/class/block/" + disk + "/queue/nr_requests")
	if err != nil {
		return analyzer.Capability{Name: "queue depth", Value: "unavailable", Weight: 3, Status: analyzer.StatusSkip}
	}
	return analyzer.Capability{Name: "queue depth", Value: v, Weight: 3, Status: analyzer.StatusOk}
}
