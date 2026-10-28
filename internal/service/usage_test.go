package service

import (
	"testing"
	"time"
)

// TestParseUsageEmptyField keeps the header of a provider whose account never
// expires: it sends "expire=" with nothing after it.
func TestParseUsageEmptyField(t *testing.T) {
	usage, err := ParseUsage("upload=1; download=2; total=300; expire=")
	if err != nil {
		t.Fatalf("ParseUsage: %v", err)
	}
	if usage.Upload != 1 || usage.Download != 2 || usage.Total != 300 || !usage.Expire.Equal(time.Time{}) {
		t.Fatalf("usage = %+v, want upload 1, download 2, total 300 and no expiry", usage)
	}
	if _, err := ParseUsage("total=abc"); err == nil {
		t.Fatal("ParseUsage accepted a field that is not a number")
	}
}
