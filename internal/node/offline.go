package node

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/betterbotbureau/linkus/internal/identity"
	"github.com/betterbotbureau/linkus/internal/protocol"
)

// The offline homepage: the file offline.html in the
// public folder IS the configuration. While the node runs, this watcher
// mirrors it to Link.us — present and changed: upload; removed: delete.
// The last mirrored state is remembered in the data directory so restarts
// send nothing.

const (
	// OfflineFileName is the owner's offline homepage in the public folder.
	OfflineFileName = "offline.html"
	offlineHashFile = "offline_hash" // data dir: hex hash of the mirrored page, "" = none
	offlinePoll     = 2 * time.Second
	offlineRetry    = 30 * time.Second
)

// WatchOfflinePage mirrors the offline homepage to the server until ctx
// ends. Run it on its own goroutine, after registration.
func WatchOfflinePage(ctx context.Context, server string, id *identity.Identity, publicDir func() string, dataDir string, log *slog.Logger) {
	mirrored := readMirroredHash(dataDir)
	var nextTry time.Time
	warnedOversize := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(offlinePoll):
		}
		if time.Now().Before(nextTry) {
			continue
		}

		content, err := os.ReadFile(filepath.Join(publicDir(), OfflineFileName))
		switch {
		case err == nil && len(content) > protocol.MaxOfflineBytes:
			sum := hashHex(content)
			if warnedOversize != sum {
				log.Warn("offline.html is over the size cap and was not uploaded",
					"size", len(content), "cap", protocol.MaxOfflineBytes)
				warnedOversize = sum
			}
			continue
		case err == nil:
			sum := hashHex(content)
			if sum == mirrored {
				continue
			}
			if err := sendOffline(ctx, server, id, http.MethodPut, content); err != nil {
				log.Warn("offline page upload failed, will retry", "error", err)
				nextTry = time.Now().Add(offlineRetry)
				continue
			}
			log.Info("offline page uploaded", "bytes", len(content))
			mirrored = sum
			saveMirroredHash(dataDir, sum, log)
		case os.IsNotExist(err):
			if mirrored == "" {
				continue
			}
			if err := sendOffline(ctx, server, id, http.MethodDelete, nil); err != nil {
				log.Warn("offline page removal failed, will retry", "error", err)
				nextTry = time.Now().Add(offlineRetry)
				continue
			}
			log.Info("offline page removed")
			mirrored = ""
			saveMirroredHash(dataDir, "", log)
		default:
			log.Warn("cannot read offline.html", "error", err)
			nextTry = time.Now().Add(offlineRetry)
		}
	}
}

// sendOffline performs one signed PUT or DELETE of the offline page.
func sendOffline(ctx context.Context, server string, id *identity.Identity, method string, content []byte) error {
	ts := time.Now().UTC().Format(time.RFC3339)
	req := protocol.OfflineRequest{
		Timestamp: ts,
		Signature: base64.StdEncoding.EncodeToString(
			id.Sign(protocol.OfflineSigningBytes(method, id.NodeID, ts, content))),
	}
	if method == http.MethodPut {
		req.Content = base64.StdEncoding.EncodeToString(content)
	}
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	url := strings.TrimSuffix(server, "/") + "/v1/nodes/" + id.NodeID + "/offline"
	httpReq, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("server replied %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	return nil
}

func hashHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func readMirroredHash(dataDir string) string {
	data, err := os.ReadFile(filepath.Join(dataDir, offlineHashFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func saveMirroredHash(dataDir, sum string, log *slog.Logger) {
	if err := os.WriteFile(filepath.Join(dataDir, offlineHashFile), []byte(sum+"\n"), 0o600); err != nil {
		log.Warn("could not remember the offline page state", "error", err)
	}
}
