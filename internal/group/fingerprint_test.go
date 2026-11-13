package group

import "testing"

func TestFingerprintCollapsesDigits(t *testing.T) {
	a := Fingerprint("/subscriptions/SUB/resourceGroups/rg/providers/Microsoft.Web/sites/api",
		"System.NullReferenceException", "OrderService.Place", "failed 12 times on attempt 3")
	b := Fingerprint("/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Web/sites/api",
		"System.NullReferenceException", "OrderService.Place", "failed 99 times on attempt 8")
	if a != b {
		t.Fatalf("digits and resource case should group: %x vs %x", a, b)
	}
	c := Fingerprint("/subscriptions/sub/resourceGroups/rg/providers/Microsoft.Web/sites/api",
		"System.NullReferenceException", "OrderService.Place", "timed out talking to sql")
	if a == c {
		t.Fatal("different messages must not group")
	}
}

func TestFingerprintFrameSplits(t *testing.T) {
	a := Fingerprint("res", "Ex", "A.Method", "boom")
	b := Fingerprint("res", "Ex", "B.Method", "boom")
	if a == b {
		t.Fatal("frames must split fingerprints")
	}
}
