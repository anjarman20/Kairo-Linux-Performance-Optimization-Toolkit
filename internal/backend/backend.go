// Package backend abstracts every mutation the optimizer makes, so production
// uses the real filesystem while tests run against a fake in-memory backend.
package backend

import (
	"context"
	"os"
)

// Backend is the single dependency injection point for system reads and
// writes. The real backend touches /proc, /sys and filesystem state; tests
// substitute a fake and therefore never require root.
type Backend interface {
	Read(ctx context.Context, path string) ([]byte, error)
	Write(ctx context.Context, path string, data []byte) error
}

// Real is the production backend backed by the filesystem.
type Real struct{}

// Read implements Backend.
func (Real) Read(ctx context.Context, path string) ([]byte, error) {
	return os.ReadFile(path)
}

// Write implements Backend. Mode is ignored for proc/sysfs pseudo-files;
// it matters for real files where 0644 is the safe default.
func (Real) Write(ctx context.Context, path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}
