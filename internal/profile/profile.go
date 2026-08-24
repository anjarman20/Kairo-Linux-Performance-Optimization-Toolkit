// Package profile defines and validates Kairo workload profiles.
//
// Profiles are declarative: name, description, and a targets map of
// area -> parameter -> desired value. Unknown areas, unknown parameters, and
// empty values are rejected with a path to the offending field so operators
// and authors get precise feedback instead of silent misconfiguration.
package profile

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed profiles/*.yaml
var embedded embed.FS

// Target is one area's parameters, e.g. cpu -> governor -> performance.
type Target map[string]string

// Profile is a validated workload profile.
type Profile struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description"`
	Targets     map[string]Target `yaml:"targets"`
}

// knownParams holds the valid parameters per target area. kernel is present
// so a typo like targets.kernel.* is caught as unknown rather than attached.
var knownParams = map[string]map[string]bool{
	"cpu":     {"governor": true},
	"memory":  {"swappiness": true, "vfs_cache_pressure": true},
	"network": {"congestion_control": true, "qdisc": true},
	"storage": {"scheduler": true},
	"kernel":  {},
}

// Validate rejects profiles with unknown fields or empty values. Errors carry
// a dotted path (targets.network.congestion_control) for precise reporting.
func Validate(p *Profile) error {
	if p.Name == "" {
		return fmt.Errorf("profile: missing required field 'name'")
	}
	if p.Targets == nil {
		return nil // a profile may legitimately declare no targets (balanced)
	}
	for area, params := range p.Targets {
		keys, known := knownParams[area]
		if !known {
			return fmt.Errorf("targets.%s: unknown area", area)
		}
		for k, v := range params {
			if !keys[k] {
				return fmt.Errorf("targets.%s.%s: unknown parameter", area, k)
			}
			if strings.TrimSpace(v) == "" {
				return fmt.Errorf("targets.%s.%s: empty value", area, k)
			}
		}
	}
	return nil
}

// LoadAll reads and validates the embedded profiles, sorted by name.
func LoadAll() ([]Profile, error) {
	entries, err := fs.ReadDir(embedded, "profiles")
	if err != nil {
		return nil, err
	}
	var out []Profile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := fs.ReadFile(embedded, "profiles/"+e.Name())
		if err != nil {
			return nil, err
		}
		var p Profile
		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true) // reject unknown top-level fields
		if err := dec.Decode(&p); err != nil {
			return nil, fmt.Errorf("profiles/%s: %v", e.Name(), err)
		}
		if err := Validate(&p); err != nil {
			return nil, fmt.Errorf("profiles/%s: %v", e.Name(), err)
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Get returns the named profile from the embedded set.
func Get(name string) (Profile, error) {
	all, err := LoadAll()
	if err != nil {
		return Profile{}, err
	}
	for _, p := range all {
		if p.Name == name {
			return p, nil
		}
	}
	return Profile{}, fmt.Errorf("profile not found: %s", name)
}

// Names lists embedded profile names in stable order.
func Names() ([]string, error) {
	all, err := LoadAll()
	if err != nil {
		return nil, err
	}
	names := make([]string, len(all))
	for i, p := range all {
		names[i] = p.Name
	}
	return names, nil
}
