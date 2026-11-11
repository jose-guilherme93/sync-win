package collectors

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollectNetworkDoesNotMislabelTxCollisionsAsRx(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "net"), 0o755); err != nil {
		t.Fatal(err)
	}
	line := "  eth0: 1 2 3 4 5 6 7 8 9 10 11 12 13 999 100 11\n"
	data := "Inter-|   Receive                                                |  Transmit\n" +
		" face |bytes packets errs drop fifo frame compressed multicast|bytes packets errs drop fifo colls carrier compressed\n" +
		line
	if err := os.WriteFile(filepath.Join(root, "net", "dev"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	ifaces := CollectNetwork(root)
	if len(ifaces) != 1 {
		t.Fatalf("got %d ifaces, want 1: %+v", len(ifaces), ifaces)
	}
	if ifaces[0].RXCollisions != 0 {
		t.Errorf("rx_collisions = %d, want 0", ifaces[0].RXCollisions)
	}
}
