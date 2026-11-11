package collectors

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollectMemoryExpandedReadsSReclaimable(t *testing.T) {
	root := t.TempDir()
	data := "MemTotal: 1000 kB\nMemFree: 100 kB\nBuffers: 50 kB\nCached: 200 kB\nSReclaimable: 30 kB\n"
	if err := os.WriteFile(filepath.Join(root, "meminfo"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	m := CollectMemoryExpanded(root)
	if m.SReclaimableBytes != 30*1024 || m.ReclaimableBytes != 30*1024 {
		t.Errorf("reclaimable = %d/%d, want %d", m.SReclaimableBytes, m.ReclaimableBytes, 30*1024)
	}
}
