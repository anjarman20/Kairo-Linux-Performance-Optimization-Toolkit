package profile

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func good() *Profile {
	return &Profile{
		Name: "gaming",
		Targets: map[string]Target{
			"cpu":     {"governor": "performance"},
			"network": {"congestion_control": "bbr"},
		},
	}
}

func TestValidateAcceptsValid(t *testing.T) {
	if err := Validate(good()); err != nil {
		t.Fatalf("expected valid profile: %v", err)
	}
}

func TestValidateMissingName(t *testing.T) {
	p := good()
	p.Name = ""
	if err := Validate(p); err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestValidateUnknownAreaHasPath(t *testing.T) {
	p := good()
	p.Targets = map[string]Target{"gpu": {"clocks": "boost"}}
	err := Validate(p)
	if err == nil || !strings.Contains(err.Error(), "targets.gpu") {
		t.Fatalf("want path targets.gpu in error, got: %v", err)
	}
}

func TestValidateUnknownParameterHasPath(t *testing.T) {
	p := good()
	p.Targets["network"]["cc"] = "bbr"
	err := Validate(p)
	if err == nil || !strings.Contains(err.Error(), "targets.network.cc") {
		t.Fatalf("want path targets.network.cc in error, got: %v", err)
	}
}

func TestValidateEmptyValue(t *testing.T) {
	p := good()
	p.Targets["cpu"] = Target{"governor": " "}
	err := Validate(p)
	if err == nil || !strings.Contains(err.Error(), "empty value") {
		t.Fatalf("want empty-value error, got: %v", err)
	}
}

func TestValidateNilTargetsAllowed(t *testing.T) {
	p := good()
	p.Targets = nil
	if err := Validate(p); err != nil {
		t.Fatalf("nil targets should be valid: %v", err)
	}
}

func TestUnknownTopLevelFieldRejected(t *testing.T) {
	raw := "name: hack\nunknown_field: true\ntargets: {}\n"
	var p Profile
	dec := yaml.NewDecoder(strings.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&p); err == nil {
		t.Fatal("expected unknown top-level field to be rejected")
	}
}

func TestEmbeddedProfilesValidateAndNamed(t *testing.T) {
	all, err := LoadAll()
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	want := []string{"balanced", "compute", "database", "gaming", "network", "virtualization"}
	if len(all) != len(want) {
		t.Fatalf("got %d profiles, want %d", len(all), len(want))
	}
	for i, name := range want {
		if all[i].Name != name {
			t.Errorf("profile[%d].Name=%q want=%q", i, all[i].Name, name)
		}
	}
}

func TestGetMissing(t *testing.T) {
	if _, err := Get("nope"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("want not-found error, got: %v", err)
	}
}

func TestGetFound(t *testing.T) {
	p, err := Get("database")
	if err != nil {
		t.Fatalf("database profile should exist: %v", err)
	}
	if _, ok := p.Targets["storage"]["scheduler"]; !ok {
		t.Error("database profile should target storage.scheduler")
	}
	if _, ok := p.Targets["memory"]["vfs_cache_pressure"]; !ok {
		t.Error("database profile should target memory.vfs_cache_pressure")
	}
}
