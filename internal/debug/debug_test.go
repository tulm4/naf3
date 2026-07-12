package debug

import "testing"

func TestEnabled_DefaultsFalse(t *testing.T) {
	d := &Debug{}
	if d.Enabled() {
		t.Fatal("expected Enabled()=false for zero-value Debug")
	}
}

func TestEnabled_TogglesWithSet(t *testing.T) {
	d := &Debug{}
	d.Set(true)
	if !d.Enabled() {
		t.Fatal("expected Enabled()=true after Set(true)")
	}
	d.Set(false)
	if d.Enabled() {
		t.Fatal("expected Enabled()=false after Set(false)")
	}
}
