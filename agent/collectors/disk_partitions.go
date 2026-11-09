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
		"zfs":     true, "tmpfs": true,
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
		options := fields[3]

		// Skip virtual/pseudo filesystems
		if !validTypes[fsType] {
			continue
		}
		// Every user session gets its own tmpfs. Only the ones that can fill up
		// and matter to an operator are reported.
		if fsType == "tmpfs" && mount != "/tmp" && mount != "/dev/shm" && mount != "/run" {
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

		// A bind-mounted file (Docker's /etc/hosts, /etc/resolv.conf) is not a
		// filesystem: statfs on it returns the backing disk, which made a
		// container look like a 96%-full 91 GB machine.
		if info, err := os.Stat(mount); err != nil || !info.IsDir() {
			continue
		}

		var stat syscall.Statfs_t
		if err := syscall.Statfs(mount, &stat); err != nil {
			continue
		}

		totalBytes := stat.Blocks * uint64(stat.Bsize)
		freeBytes := stat.Bavail * uint64(stat.Bsize)
		reservedBytes := (stat.Bfree - stat.Bavail) * uint64(stat.Bsize)
		usedBytes := totalBytes - freeBytes
		var usedPercent float64
		if totalBytes > 0 {
			usedPercent = float64(usedBytes) * 100 / float64(totalBytes)
		}

		// Inode exhaustion fills a filesystem that still has free blocks, which
		// is common under /var and invisible in a bytes-only view.
		var inodesTotal, inodesUsed, inodesFree uint64
		var inodesPct float64
		if stat.Files > 0 {
			inodesTotal = stat.Files
			inodesFree = stat.Ffree
			inodesUsed = inodesTotal - inodesFree
			inodesPct = float64(inodesUsed) * 100 / float64(inodesTotal)
		}

		partitions = append(partitions, DiskPartition{
			Mount:         mount,
			Device:        device,
			FSType:        fsType,
			Options:       options,
			ReadOnly:      strings.Contains(","+options+",", ",ro,"),
			TotalBytes:    totalBytes,
			UsedBytes:     usedBytes,
			FreeBytes:     freeBytes,
			AvailBytes:    freeBytes,
			ReservedBytes: reservedBytes,
			UsedPercent:   usedPercent,
			InodesTotal:   inodesTotal,
			InodesUsed:    inodesUsed,
			InodesFree:    inodesFree,
			InodesUsedPct: inodesPct,
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
