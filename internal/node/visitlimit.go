package node

import (
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/betterbotbureau/linkus/internal/protocol"
)

// The owner chooses how many visitors the node serves at once; one
// internet address may hold 40 percent of that, so a single person or
// script can't take every place, while a home or office sharing one
// address still fits several tabs.

const visitLimitFile = "visitlimit"

// LoadMaxVisits reports the owner's limit; missing or unreadable means
// the default.
func LoadMaxVisits(dataDir string) int {
	data, err := os.ReadFile(filepath.Join(dataDir, visitLimitFile))
	if err != nil {
		return protocol.DefaultMaxVisits
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || !ValidMaxVisits(n) {
		return protocol.DefaultMaxVisits
	}
	return n
}

// SaveMaxVisits records the owner's limit.
func SaveMaxVisits(dataDir string, n int) error {
	return os.WriteFile(filepath.Join(dataDir, visitLimitFile), []byte(strconv.Itoa(n)+"\n"), 0o600)
}

// ValidMaxVisits reports whether n is a limit the owner may choose.
func ValidMaxVisits(n int) bool {
	return n >= protocol.MinMaxVisits && n <= protocol.MaxMaxVisits
}

// PerAddressVisits is how many of total visits one address may hold:
// 40 percent, at least one.
func PerAddressVisits(total int) int {
	return max(1, total*40/100)
}

// addressKey groups a visitor's IP for the per-address cap. An IPv6
// home network gets a whole /64, so its devices count as one address.
// Anything unparsable gives "", which no per-address cap applies to.
func addressKey(from string) string {
	ip, err := netip.ParseAddr(strings.TrimSpace(from))
	if err != nil {
		return ""
	}
	ip = ip.Unmap()
	if ip.Is4() {
		return ip.String()
	}
	p, err := ip.Prefix(64)
	if err != nil {
		return ""
	}
	return p.String()
}

// visitSlots counts open visits, in total and per address, against a
// limit that can change while the node runs.
type visitSlots struct {
	mu     sync.Mutex
	max    int
	open   int
	byAddr map[string]int
}

func newVisitSlots(total int) *visitSlots {
	return &visitSlots{max: total, byAddr: map[string]int{}}
}

func (v *visitSlots) setMax(n int) {
	v.mu.Lock()
	v.max = n
	v.mu.Unlock()
}

func (v *visitSlots) limits() (total, perAddr int) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.max, PerAddressVisits(v.max)
}

// take claims a place for a visit from addr, or reports why not
// ("full" or "address"). Lowering the limit never ends open visits; new
// ones wait until enough have closed.
func (v *visitSlots) take(addr string) (ok bool, reason string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.open >= v.max {
		return false, "full"
	}
	if addr != "" && v.byAddr[addr] >= PerAddressVisits(v.max) {
		return false, "address"
	}
	v.open++
	if addr != "" {
		v.byAddr[addr]++
	}
	return true, ""
}

func (v *visitSlots) release(addr string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.open--
	if addr != "" {
		if v.byAddr[addr]--; v.byAddr[addr] <= 0 {
			delete(v.byAddr, addr)
		}
	}
}
