package node

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// The request log: one JSON line per visit in the data
// directory, so the owner can see what remote visitors asked for and how
// connections went, without a log daemon. Disk use is bounded: at the cap
// the file rotates once to .old.

const (
	requestLogFile    = "requests.log"
	requestLogMaxSize = 1 << 20 // rotate at 1 MB
)

// VisitEntry is one logged visit.
type VisitEntry struct {
	Time    time.Time `json:"time"`
	Visit   string    `json:"visit"`
	Outcome string    `json:"outcome"`
	ICE     string    `json:"ice"`
	IP      string    `json:"ip,omitempty"` // visitor address from the selected ICE pair
	Paths   []string  `json:"paths,omitempty"`
	Bytes   int64     `json:"bytes"`
	Secs    float64   `json:"secs,omitempty"` // visit duration, 0.1s resolution
}

// RequestLog appends visit entries to the data directory, rotating at the
// size cap. Safe for concurrent use.
type RequestLog struct {
	mu   sync.Mutex
	path string
	log  *slog.Logger
}

// OpenRequestLog prepares the log inside dataDir.
func OpenRequestLog(dataDir string, log *slog.Logger) *RequestLog {
	return &RequestLog{path: filepath.Join(dataDir, requestLogFile), log: log}
}

// Add appends one entry. Failures are logged, never fatal: the log is a
// convenience, not a dependency.
func (l *RequestLog) Add(e VisitEntry) {
	if l == nil {
		return
	}
	data, err := json.Marshal(e)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if info, err := os.Stat(l.path); err == nil && info.Size() >= requestLogMaxSize {
		if err := os.Rename(l.path, l.path+".old"); err != nil {
			l.log.Warn("could not rotate the request log", "error", err)
		}
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		l.log.Warn("could not write the request log", "error", err)
		return
	}
	defer f.Close()
	f.Write(append(data, '\n')) //nolint:errcheck // best effort by design
}

// TailRequestLog returns the last n entries, reading the rotated file too
// when the current one is short. Malformed lines are skipped.
func TailRequestLog(dataDir string, n int) ([]VisitEntry, error) {
	path := filepath.Join(dataDir, requestLogFile)
	entries := readEntries(path + ".old")
	entries = append(entries, readEntries(path)...)
	if len(entries) > n {
		entries = entries[len(entries)-n:]
	}
	return entries, nil
}

func readEntries(path string) []VisitEntry {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var entries []VisitEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		var e VisitEntry
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			entries = append(entries, e)
		}
	}
	return entries
}

// CountRequestLog returns how many visits the log holds in total, both
// files included. Cheap line counting; malformed lines count anyway (every
// written line was a visit).
func CountRequestLog(dataDir string) int {
	n := 0
	for _, path := range []string{
		filepath.Join(dataDir, requestLogFile+".old"),
		filepath.Join(dataDir, requestLogFile),
	} {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			n++
		}
		f.Close()
	}
	return n
}
