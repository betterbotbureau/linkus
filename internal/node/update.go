package node

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/betterbotbureau/linkus/internal/protocol"
)

// UpdateInfo is what the owner is told about releases.
type UpdateInfo struct {
	Current   string `json:"current"`
	Latest    string `json:"latest,omitempty"`
	URL       string `json:"url,omitempty"`
	Available bool   `json:"available"`
}

// ReleaseEdition is the edition the service is asked about: "cli", the
// open-source command-line node, unless the desktop app says otherwise.
var ReleaseEdition = "cli"

// CheckRelease asks the service for the latest published release.
func CheckRelease(ctx context.Context, server string) (protocol.ReleaseInfo, error) {
	var info protocol.ReleaseInfo
	u := strings.TrimSuffix(server, "/") + "/v1/release"
	if ReleaseEdition != "" {
		u += "?edition=" + ReleaseEdition
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return info, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return info, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return info, fmt.Errorf("server replied %s", resp.Status)
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, protocol.MaxBodyBytes)).Decode(&info)
	return info, err
}

// Compare tells the owner whether latest is newer than the running
// version. Only a release page on GitHub or a download page on link.us is
// passed on as a link.
func Compare(current string, rel protocol.ReleaseInfo) UpdateInfo {
	u := UpdateInfo{Current: current, Latest: rel.Latest}
	if strings.HasPrefix(rel.URL, "https://github.com/") || strings.HasPrefix(rel.URL, "https://link.us/") {
		u.URL = rel.URL
	}
	u.Available = u.URL != "" && NewerVersion(current, rel.Latest)
	return u
}

// NewerVersion reports whether latest is a newer release than current.
// Versions are tags like v1.2 or v1.2.3; a development build described
// by git (v1.1-3-gabc1234) counts as its base release, and anything
// unparseable — "dev" included — never reports an update.
func NewerVersion(current, latest string) bool {
	cur, ok1 := parseVersion(current)
	lat, ok2 := parseVersion(latest)
	if !ok1 || !ok2 {
		return false
	}
	for i := range cur {
		if lat[i] != cur[i] {
			return lat[i] > cur[i]
		}
	}
	return false
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	v, _, _ = strings.Cut(v, "-")
	v, ok := strings.CutPrefix(v, "v")
	if !ok || v == "" {
		return out, false
	}
	parts := strings.Split(v, ".")
	if len(parts) > 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// UpdateWatcher checks for a newer release at startup and daily, and
// keeps the last answer for the settings page.
type UpdateWatcher struct {
	mu   sync.Mutex
	info UpdateInfo
}

// NewUpdateWatcher starts with no release known.
func NewUpdateWatcher(current string) *UpdateWatcher {
	return &UpdateWatcher{info: UpdateInfo{Current: current}}
}

// Info returns the latest known answer.
func (w *UpdateWatcher) Info() UpdateInfo {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.info
}

// Run checks until ctx ends. onNew is called once per newly seen release
// that is newer than the running version.
func (w *UpdateWatcher) Run(ctx context.Context, server string, log *slog.Logger, onNew func(UpdateInfo)) {
	announced := ""
	for {
		cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		rel, err := CheckRelease(cctx, server)
		cancel()
		if err == nil {
			w.mu.Lock()
			w.info = Compare(w.info.Current, rel)
			info := w.info
			w.mu.Unlock()
			if info.Available && info.Latest != announced {
				announced = info.Latest
				if onNew != nil {
					onNew(info)
				}
			}
		} else if ctx.Err() == nil {
			log.Debug("release check failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(jitter(24 * time.Hour)):
		}
	}
}
