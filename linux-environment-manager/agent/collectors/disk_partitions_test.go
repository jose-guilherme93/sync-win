package collectors

import "testing"

// The dashboard showed one row per mount point, so an ext4 system reported the
// same device once for /, /home, /root, /srv and every other directory on it.
// Each filesystem must appear exactly once.
func TestCollectDiskPartitionsDedupesByDevice(t *testing.T) {
	parts := CollectDiskPartitions()
	if len(parts) == 0 {
		t.Skip("no eligible filesystem in this environment")
	}

	seen := map[string]string{}
	for _, p := range parts {
		if p.TotalBytes == 0 {
			t.Errorf("mount %q reported zero total bytes", p.Mount)
		}
		if p.UsedPercent < 0 || p.UsedPercent > 100 {
			t.Errorf("mount %q used_percent = %v, outside 0-100", p.Mount, p.UsedPercent)
		}
		if p.Device == "" {
			continue // btrfs subvolumes legitimately share a device
		}
		if first, dup := seen[p.Device]; dup {
			t.Errorf("device %s reported twice: mount %q and %q", p.Device, first, p.Mount)
			continue
		}
		seen[p.Device] = p.Mount
	}
}
