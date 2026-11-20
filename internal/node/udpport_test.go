package node

import (
	"os"
	"path/filepath"
	"testing"
)

func TestUDPPortRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if got := LoadUDPPort(dir); got != 0 {
		t.Fatalf("missing file: port %d, want 0", got)
	}
	if err := SaveUDPPort(dir, 61234); err != nil {
		t.Fatal(err)
	}
	if got := LoadUDPPort(dir); got != 61234 {
		t.Fatalf("port %d, want 61234", got)
	}
	// Garbage and out-of-range values mean "pick a fresh one".
	for _, bad := range []string{"not a port\n", "80\n", "70000\n", ""} {
		if err := os.WriteFile(filepath.Join(dir, udpPortFile), []byte(bad), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := LoadUDPPort(dir); got != 0 {
			t.Fatalf("%q: port %d, want 0", bad, got)
		}
	}
}
