package node

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/coder/websocket"

	"github.com/betterbotbureau/linkus/internal/protocol"
)

// Two computers, one identity. After the settings folder is copied to
// another computer, the old copy of the node may still run. The rule: the
// copy that is running keeps running. A copy started while another one is
// connected and answering is refused by the service and stays offline,
// saying so — it does not switch over by itself, now or later. To move
// the node, quit the running copy, then start (or restart) the other one.

const displacedFile = "displaced"

// ErrInUse reports that another copy of this node is already running, so
// the service refused this one.
var ErrInUse = errors.New("another copy of this node is already running; this one stays offline")

func isInUse(err error) bool {
	return websocket.CloseStatus(err) == websocket.StatusCode(protocol.CloseInUse)
}

// MarkDisplaced records that another copy of this node is running, so
// this one stays offline until it is started again.
func MarkDisplaced(dataDir string) error {
	return os.WriteFile(filepath.Join(dataDir, displacedFile), []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600)
}

// Displaced reports whether this copy was refused because another copy
// is running, and since when; the zero time when it was not.
func Displaced(dataDir string) (bool, time.Time) {
	data, err := os.ReadFile(filepath.Join(dataDir, displacedFile))
	if err != nil {
		return false, time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, strings.TrimSpace(string(data)))
	return true, t
}

// ClearDisplaced forgets an earlier refusal. Every start of the node does
// this, so a restarted copy tries again — and runs if no other copy does.
func ClearDisplaced(dataDir string) error {
	if err := os.Remove(filepath.Join(dataDir, displacedFile)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
