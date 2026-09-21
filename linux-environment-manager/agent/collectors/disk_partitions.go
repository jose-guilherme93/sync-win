package collectors

import (
	"bufio"
	"os"
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
	seen := map[string]bool{}

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
		if seen[mount] {
			continue
		}
		seen[mount] = true
		// Skip loop/ram devices
		if strings.HasPrefix(device, "/dev/loop") || strings.HasPrefix(device, "/dev/ram") {
			continue
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

	// Sort by mount depth (shorter paths first, e.g. / before /home)
	for i := 0; i < len(partitions); i++ {
		for j := i + 1; j < len(partitions); j++ {
			if strings.Count(partitions[i].Mount, "/") > strings.Count(partitions[j].Mount, "/") ||
				(strings.Count(partitions[i].Mount, "/") == strings.Count(partitions[j].Mount, "/") &&
					len(partitions[i].Mount) > len(partitions[j].Mount)) {
				partitions[i], partitions[j] = partitions[j], partitions[i]
			}
		}
	}

	return partitions
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
