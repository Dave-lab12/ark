package resources

import "testing"

func TestNormalizeMemoryLimit(t *testing.T) {
	got, err := NormalizeMemoryLimit(" 4g ")
	if err != nil {
		t.Fatalf("NormalizeMemoryLimit: %v", err)
	}
	if got != "4g" {
		t.Fatalf("NormalizeMemoryLimit = %q, want 4g", got)
	}
}

func TestNormalizeMemoryLimitRejectsInvalid(t *testing.T) {
	if _, err := NormalizeMemoryLimit("nope"); err == nil {
		t.Fatalf("expected invalid memory error")
	}
}
