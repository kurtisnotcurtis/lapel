package state

import (
	"testing"
)

func TestSetDesiredUpsertIdempotent(t *testing.T) {
	s := openTestStore(t)
	spec1, hash1 := `{"name":"p","baseURL":"http://localhost:1234/v1"}`, "h1"
	if err := s.SetDesired(KindProvider, "local", spec1, hash1); err != nil {
		t.Fatalf("SetDesired #1: %v", err)
	}
	spec2, hash2 := `{"name":"p","baseURL":"http://localhost:9/v1"}`, "h2"
	if err := s.SetDesired(KindProvider, "local", spec2, hash2); err != nil {
		t.Fatalf("SetDesired #2: %v", err)
	}
	got, err := GetDesired(s, KindProvider, "local")
	if err != nil || got == nil {
		t.Fatalf("GetDesired after upsert failed: %+v err=%v", got, err)
	}
	if got.SpecJSON != spec2 || got.DesiredHash != hash2 {
		t.Fatalf("upsert did not replace baseline: spec=%q hash=%q", got.SpecJSON, got.DesiredHash)
	}
	resources, _ := s.DesiredResources()
	if len(resources) != 1 {
		t.Fatalf("expected one resource after idempotent upsert, got %d", len(resources))
	}
}

func TestDesiredResourcesRoundTrip(t *testing.T) {
	s := openTestStore(t)
	want := map[string]string{
		KindEnvironment + "\x00" + "Ubuntu-Lean": `{"distribution":"Ubuntu-Lean","base":"ubuntu"}`,
		KindProvider + "\x00" + "local-lmstudio":  `{"name":"local-lmstudio","type":"openai-compatible","baseURL":"http://localhost:1234/v1"}`,
		KindWorkspace + "\x00" + "project":        `{"name":"project","path":"/tmp"}`,
	}
	if err := s.SetDesired(KindEnvironment, "Ubuntu-Lean", want[KindEnvironment+"\x00"+"Ubuntu-Lean"], "eh"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDesired(KindProvider, "local-lmstudio", want[KindProvider+"\x00"+"local-lmstudio"], "ph"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDesired(KindWorkspace, "project", want[KindWorkspace+"\x00"+"project"], "wh"); err != nil {
		t.Fatal(err)
	}

	resources, err := s.DesiredResources()
	if err != nil {
		t.Fatalf("DesiredResources: %v", err)
	}
	if len(resources) != 3 {
		t.Fatalf("expected 3 baselines, got %d", len(resources))
	}
	for _, r := range resources {
		key := r.Kind + "\x00" + r.Name
		got, ok := want[key]
		if !ok {
			t.Fatalf("unexpected baseline: %s/%s", r.Kind, r.Name)
		}
		if r.SpecJSON != got {
			t.Fatalf("spec_json mismatch for %s: got %q want %q", key, r.SpecJSON, got)
		}
	}
}

func TestSetDesignedUnknownKind(t *testing.T) {
	s := openTestStore(t)
	if err := s.SetDesired("bogus", "n", "{}", ""); err == nil {
		t.Fatal("expected error for unknown kind")
	}
}

// GetDesired is a tiny helper mirroring the per-kind query used elsewhere.
func GetDesired(s *Store, kind, name string) (*DesiredResource, error) {
	resources, err := s.DesiredResources()
	if err != nil {
		return nil, err
	}
	for _, r := range resources {
		if r.Kind == kind && r.Name == name {
			return r, nil
		}
	}
	return nil, nil
}
