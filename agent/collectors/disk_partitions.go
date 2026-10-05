package collectors

import (
	"bufio"
	"os"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// CollectDiskPartitions reads /proc/mounts and uses statfs to get disk space per partition.
func CollectDiskPartitions() []DiskPartition {
	mountsFile, err := os.Open("/proc/mounts")
	if err != nil {
		return nil
	}
	defer mountsFile.Close()

	// Filter real filesystem types
	validTypes := map[string]bool{
		"ext4": true, "ext3": true, "ext2": true,
		"xfs": true, "btrfs": true, "f2fs": true,
		"ntfs": true, "vfat": true, "exfat": true,
		"fuseblk": true, // NTFS via ntfs-3g
	}

	var partitions []DiskPartition
	seenMount := map[string]bool{}
	seenDevice := map[string]bool{}
	// Btrfs subvolumes are independent mounts with their own usage, so they are
	// exempt from the one-entry-per-device dedup below.
	btrfsDevices := map[string]bool{}

	scanner := bufio.NewScanner(mountsFile)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		device := fields[0]
		mount := fields[1]
		fsType := fields[2]

		// Skip virtual/pseudo filesystems
		if !validTypes[fsType] {
			continue
		}
		// Skip duplicates (same mount point)
		if seenMount[mount] {
			continue
		}
		seenMount[mount] = true
		// Skip loop/ram devices
		if strings.HasPrefix(device, "/dev/loop") || strings.HasPrefix(device, "/dev/ram") {
			continue
		}
		if fsType == "btrfs" {
			btrfsDevices[device] = true
		}

		var stat syscall.Statfs_t
		if err := syscall.Statfs(mount, &stat); err != nil {
			continue
		}

		totalBytes := stat.Blocks * uint64(stat.Bsize)
		freeBytes := stat.Bavail * uint64(stat.Bsize)
		usedBytes := totalBytes - freeBytes
		var usedPercent float64
		if totalBytes > 0 {
			usedPercent = float64(usedBytes) * 100 / float64(totalBytes)
		}

		partitions = append(partitions, DiskPartition{
			Mount:       mount,
			Device:      device,
			TotalBytes:  totalBytes,
			UsedBytes:   usedBytes,
			FreeBytes:   freeBytes,
			UsedPercent: usedPercent,
		})
	}

	// Sort by mount depth (shorter paths first, e.g. / before /home) so that when
	// one device is mounted at several points the entry that survives dedup is
	// the most representative one.
	sort.Slice(partitions, func(i, j int) bool {
		mi, mj := partitions[i].Mount, partitions[j].Mount
		di, dj := strings.Count(mi, "/"), strings.Count(mj, "/")
		if di != dj {
			return di < dj
		}
		return len(mi) < len(mj)
	})

	// One entry per filesystem. On ext4 a system reports /home, /root, /srv and
	// friends as separate mounts of the same device, which made the dashboard
	// list the same disk six times.
	deduped := make([]DiskPartition, 0, len(partitions))
	for _, p := range partitions {
		if !btrfsDevices[p.Device] {
			if seenDevice[p.Device] {
				continue
			}
			seenDevice[p.Device] = true
		}
		deduped = append(deduped, p)
	}

	return deduped
}

// FormatDiskBytes formats bytes to human-readable string.
func FormatDiskBytes(bytes uint64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
		TB = 1024 * GB
	)
	switch {
	case bytes >= TB:
		return strconv.FormatFloat(float64(bytes)/float64(TB), 'f', 1, 64) + " TB"
	case bytes >= GB:
		return strconv.FormatFloat(float64(bytes)/float64(GB), 'f', 1, 64) + " GB"
	case bytes >= MB:
		return strconv.FormatFloat(float64(bytes)/float64(MB), 'f', 1, 64) + " MB"
	case bytes >= KB:
		return strconv.FormatFloat(float64(bytes)/float64(KB), 'f', 1, 64) + " KB"
	default:
		return strconv.FormatUint(bytes, 10) + " B"
	}
}
