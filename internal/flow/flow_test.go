package flow

import "testing"

func TestLoadArmanCurren(t *testing.T) {
	f, err := Load("../../stores/arman-curren")
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Steps) != 9 {
		t.Fatalf("steps=%d want 9", len(f.Steps))
	}
	if s, _ := f.Step("8"); s.Status != StatusDraft {
		t.Fatal("step 8 must be a draft")
	}
	if d := f.Dependents("2"); len(d) != 1 || d[0].ID != "2.5" {
		t.Fatalf("dependents of 2: %+v", d)
	}
}
