package integration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"hop/internal/model"
	"hop/internal/repository/mihomo"
	"hop/internal/service"
)

// profileWithAnchors uses YAML anchors the way hand-written Clash profiles do:
// templates defined under keys of their own, which Hop does not keep, merged
// into the groups and proxies it does keep, and an anchor in one kept section
// used in another.
const profileWithAnchors = `
pr: &pr
  type: select
  proxies: [DIRECT, "node-a"]
node-defaults: &nd {type: ss, port: 8388, cipher: aes-128-gcm, password: x}
proxies:
  - {name: "node-a", server: 192.0.2.1, <<: *nd}
  - {name: "node-b", server: 192.0.2.2, <<: *nd}
proxy-groups:
  - {name: "Proxy", <<: *pr}
  - {name: "Backup", type: select, proxies: ["node-b", "Proxy"]}
rules:
  - MATCH,Proxy
`

// TestProfileWithAnchors checks that a profile whose YAML anchors are defined
// outside the sections Hop keeps still builds a config the real core accepts,
// for both rule sources, and keeps what the anchors stand for.
func TestProfileWithAnchors(t *testing.T) {
	binary := os.Getenv("HOP_MIHOMO_BINARY")
	if binary == "" {
		t.Skip("HOP_MIHOMO_BINARY is not set")
	}
	validateDir := t.TempDir()
	if err := mihomo.SeedGeodata(filepath.Join("..", "..", "resources", "geodata"), validateDir); err != nil {
		t.Fatal(err)
	}
	validator := mihomo.NewConfigValidator(binary, validateDir, 30*time.Second)
	settings := service.RuntimeSettings{
		MixedPort: testMixedPort, ControllerAddr: testController, ControllerSecret: testSecret,
		BindAddress: "127.0.0.1", LogLevel: "warning", BootstrapGroupName: "Hop", Mode: model.ModeRule,
	}
	for _, rules := range []model.RuleSource{model.RulesProvider, model.RulesHop} {
		config, err := service.RuntimeConfig([]byte(profileWithAnchors), rules, nil, settings)
		if err != nil {
			t.Errorf("%s: build config: %v", rules, err)
			continue
		}
		if err := validator.ValidateWithGeodata(context.Background(), config, validateDir); err != nil {
			t.Errorf("%s: the core refused the config: %v\n%s", rules, err, config)
		}
	}
}
