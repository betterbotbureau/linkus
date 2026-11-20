package node

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The publish switch: a file in the data directory,
// so the CLI, the owner and an agent can all flip it, and a running node
// notices without IPC. Unpublished means invisible to the world — no
// presence, no answering — while the local preview keeps working.

const (
	publishFile  = "published"
	publishPoll  = 2 * time.Second
	publishOn    = "on"
	publishOff   = "off"
	publishOnB   = publishOn + "\n"
	publishOffB  = publishOff + "\n"
	publishPerms = 0o600
)

// LoadPublished reports the publish state. A missing or unreadable file
// means published: identities that predate the switch stay live.
func LoadPublished(dataDir string) bool {
	data, err := os.ReadFile(filepath.Join(dataDir, publishFile))
	if err != nil {
		return true
	}
	return strings.TrimSpace(string(data)) != publishOff
}

// SavePublished writes the publish state.
func SavePublished(dataDir string, on bool) error {
	content := publishOffB
	if on {
		content = publishOnB
	}
	return os.WriteFile(filepath.Join(dataDir, publishFile), []byte(content), publishPerms)
}

// EnsurePublishDefault writes the initial state once: a brand-new identity
// starts unpublished (going live is a deliberate flip), while an existing
// identity that predates the switch is recorded as published.
func EnsurePublishDefault(dataDir string, newIdentity bool) error {
	path := filepath.Join(dataDir, publishFile)
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return SavePublished(dataDir, !newIdentity)
}

// PublishSupervisor watches the publish state and keeps start running (with
// a cancellable context) exactly while published — and not refused
// because another copy of the node is running (see displaced.go). start must
// return when its context ends. The supervisor returns when ctx ends.
func PublishSupervisor(ctx context.Context, dataDir string, start func(context.Context), log *slog.Logger) {
	var stop func() // cancels the running start and waits for it
	for {
		displaced, _ := Displaced(dataDir)
		published := LoadPublished(dataDir) && !displaced
		switch {
		case published && stop == nil:
			log.Info("published: going online")
			subCtx, cancel := context.WithCancel(ctx)
			done := make(chan struct{})
			go func() {
				defer close(done)
				start(subCtx)
			}()
			stop = func() {
				cancel()
				<-done
			}
		case !published && stop != nil:
			if displaced {
				log.Warn("another copy of this node is running on another computer; staying offline here (to run it here: quit the other copy, then restart Link Node here)")
			} else {
				log.Info("unpublished: going offline (local preview keeps working)")
			}
			stop()
			stop = nil
		}
		select {
		case <-ctx.Done():
			if stop != nil {
				stop()
			}
			return
		case <-time.After(publishPoll):
		}
	}
}
