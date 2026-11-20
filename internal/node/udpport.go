package node

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// The WebRTC UDP port is kept stable across runs (one fixed local
// UDP port). A stable port means the router refreshes one mapping entry
// instead of accumulating a new one per run — unclean exits were filling
// small UPnP tables — and NATs see consistent behavior. The first run picks
// a random port and remembers it here; if it is ever taken, a new one is
// picked and saved.

const udpPortFile = "webrtc_port"

// LoadUDPPort returns the remembered WebRTC port, or 0 (meaning: pick one).
func LoadUDPPort(dataDir string) int {
	data, err := os.ReadFile(filepath.Join(dataDir, udpPortFile))
	if err != nil {
		return 0
	}
	port, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || port < 1024 || port > 65535 {
		return 0
	}
	return port
}

// SaveUDPPort remembers the WebRTC port for the next run. Best effort.
func SaveUDPPort(dataDir string, port int) error {
	return os.WriteFile(filepath.Join(dataDir, udpPortFile),
		[]byte(strconv.Itoa(port)+"\n"), 0o600)
}
