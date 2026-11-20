//go:build !windows

package node

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// On macOS and Linux the backup is a private file in the system's
// per-user application folder (~/Library/Application Support/LinkNodeKeys
// or ~/.config/LinkNodeKeys), outside ~/Link — and outside the pre-v1.4
// settings folder (…/LinkNode), which a node may still use or migrate.
// Not the macOS Keychain: an ad-hoc signed app gets a new signature with
// every release, and the Keychain would ask for the login password after
// each update.

// keyBackupDir is a variable so tests can use a temporary folder.
var keyBackupDir = func() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "LinkNodeKeys"), nil
}

func keyBackupPath(dataDir string) (string, error) {
	dir, err := keyBackupDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, backupName(dataDir)+".key"), nil
}

func loadKeyBackup(dataDir string) ([]byte, error) {
	p, err := keyBackupPath(dataDir)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(p)
}

func writeKeyBackup(dataDir string, data []byte) error {
	p, err := keyBackupPath(dataDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func deleteKeyBackup(dataDir string) error {
	p, err := keyBackupPath(dataDir)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
