package mihomo

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestValidateWithGeodata makes sure the real core's -t reads the databases
// from the directory a geodata update stages them in: it accepts the bundled
// ones and rejects a corrupt file, without downloading a replacement.
func TestValidateWithGeodata(t *testing.T) {
	binary := os.Getenv("HOP_MIHOMO_BINARY")
	if binary == "" {
		t.Skip("HOP_MIHOMO_BINARY is not set")
	}
	dir := t.TempDir()
	for _, name := range []string{"GeoSite.dat", "geoip.metadb"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "resources", "geodata", name))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	config := []byte(`mixed-port: 0
mode: rule
proxies: []
proxy-groups: []
rules:
  - GEOSITE,private,DIRECT
  - GEOIP,CN,DIRECT
  - MATCH,DIRECT
`)
	validator := NewConfigValidator(binary, t.TempDir(), 30*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := validator.ValidateWithGeodata(ctx, config, dir); err != nil {
		t.Fatalf("the real core rejected the bundled geodata: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "GeoSite.dat"), []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validator.ValidateWithGeodata(ctx, config, dir); err == nil {
		t.Fatal("the real core accepted a corrupt GeoSite.dat")
	}
}
