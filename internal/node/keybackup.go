package node

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/betterbotbureau/linkus/internal/identity"
)

// The key backup. The node's private key is its address: lose it and the
// node gets a new ID. It lives in identity.json in the settings folder,
// and a second copy is kept outside that folder, tied to this computer
// and user account — Windows Credential Manager, or a private file in the
// system's per-user application folder elsewhere. Deleting the settings
// folder then resets the settings but keeps the address. The copy is a
// backup only: if it cannot be written, nothing else is affected.

// keyBackup is what the backup holds.
type keyBackup struct {
	DataDir    string `json:"data_dir"`
	PrivateKey string `json:"private_key"`
}

// backupName names the backup for one settings folder, so two nodes run
// by the same user with different folders never share a key.
func backupName(dataDir string) string {
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		abs = dataDir
	}
	sum := sha256.Sum256([]byte(abs))
	return hex.EncodeToString(sum[:8])
}

func encodeBackup(dataDir string, priv ed25519.PrivateKey) ([]byte, error) {
	return json.Marshal(keyBackup{DataDir: dataDir, PrivateKey: base64.StdEncoding.EncodeToString(priv)})
}

func decodeBackup(data []byte) (ed25519.PrivateKey, error) {
	var b keyBackup
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(b.PrivateKey)
	if err != nil || len(key) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid key in backup")
	}
	return ed25519.PrivateKey(key), nil
}

// LoadIdentity loads the node's identity from dataDir. When the settings
// folder has none but the backup does, the identity is restored from the
// backup (same key, so the same ID and address); only when neither exists
// is a new one created. A damaged identity file is an error, never a
// silent new identity.
func LoadIdentity(dataDir string, log *slog.Logger) (id *identity.Identity, created, restored bool, err error) {
	id, err = identity.Load(dataDir)
	switch {
	case err == nil:
		ensureKeyBackup(dataDir, id, log)
		return id, false, false, nil
	case !errors.Is(err, os.ErrNotExist):
		return nil, false, false, err
	}
	if priv, ok := readKeyBackup(dataDir); ok {
		id, err = identity.Restore(dataDir, priv)
		if err != nil {
			return nil, false, false, fmt.Errorf("restore identity from backup: %w", err)
		}
		return id, false, true, nil
	}
	id, created, err = identity.LoadOrCreate(dataDir)
	if err != nil {
		return nil, false, false, err
	}
	ensureKeyBackup(dataDir, id, log)
	return id, created, false, nil
}

// ensureKeyBackup writes the backup when it is missing or holds another
// key (after a restore from an export, the identity file wins).
func ensureKeyBackup(dataDir string, id *identity.Identity, log *slog.Logger) {
	if priv, ok := readKeyBackup(dataDir); ok && priv.Equal(id.PrivateKey) {
		return
	}
	data, err := encodeBackup(dataDir, id.PrivateKey)
	if err == nil {
		err = writeKeyBackup(dataDir, data)
	}
	if err != nil {
		log.Warn("could not back up the node's key outside the settings folder", "error", err)
	}
}

func readKeyBackup(dataDir string) (ed25519.PrivateKey, bool) {
	data, err := loadKeyBackup(dataDir)
	if err != nil {
		return nil, false
	}
	priv, err := decodeBackup(data)
	return priv, err == nil
}

// ForgetIdentity deletes the node's identity and its backup, so the next
// start creates a new identity — and a new address. Settings stay.
func ForgetIdentity(dataDir string) error {
	if err := os.Remove(filepath.Join(dataDir, identity.FileName)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return deleteKeyBackup(dataDir)
}
