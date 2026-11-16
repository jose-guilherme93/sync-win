package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The unit files are world readable. A device token in ExecStart is a
// credential any local user can copy, so the shipped units must point at the
// 0640 token file instead.
func TestAgentUnitTemplatesUseTheTokenFile(t *testing.T) {
	for _, name := range []string{"sync-win-agent.service", "sync-win-agent-update.service"} {
		data, err := os.ReadFile(filepath.Join("..", "..", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		unit := string(data)
		if strings.Contains(unit, "{{DEVICE_TOKEN}}") {
			t.Errorf("%s still carries the token placeholder", name)
		}
		if !strings.Contains(unit, "--device-token-file") {
			t.Errorf("%s does not point at the token file", name)
		}
	}
}
