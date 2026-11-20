//go:build !windows

package node

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/betterbotbureau/linkus/internal/identity"
)

// useTempKeyBackups points the key backup at a temporary folder.
func useTempKeyBackups(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	orig := keyBackupDir
	keyBackupDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { keyBackupDir = orig })
	return dir
}

func TestIdentitySurvivesDeletedSettings(t *testing.T) {
	useTempKeyBackups(t)
	settings := filepath.Join(t.TempDir(), "settings")

	first, created, restored, err := LoadIdentity(settings, testLogger())
	if err != nil || !created || restored {
		t.Fatalf("first start: created=%v restored=%v err=%v", created, restored, err)
	}

	// The owner deletes the whole settings folder to reset the app.
	if err := os.RemoveAll(settings); err != nil {
		t.Fatal(err)
	}
	again, created, restored, err := LoadIdentity(settings, testLogger())
	if err != nil || created || !restored {
		t.Fatalf("after reset: created=%v restored=%v err=%v", created, restored, err)
	}
	if !again.PublicKey.Equal(first.PublicKey) {
		t.Fatal("the reset changed the key, and with it the address")
	}

	// A normal start afterwards is neither new nor restored.
	if _, created, restored, err = LoadIdentity(settings, testLogger()); err != nil || created || restored {
		t.Fatalf("normal start: created=%v restored=%v err=%v", created, restored, err)
	}
}

func TestDamagedIdentityIsNeverReplacedSilently(t *testing.T) {
	useTempKeyBackups(t)
	settings := t.TempDir()
	if _, _, _, err := LoadIdentity(settings, testLogger()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(settings, identity.FileName), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := LoadIdentity(settings, testLogger()); err == nil {
		t.Fatal("a damaged identity file was replaced instead of reported")
	}
}

func TestIdentityFileWinsOverBackup(t *testing.T) {
	useTempKeyBackups(t)
	settings := t.TempDir()
	if _, _, _, err := LoadIdentity(settings, testLogger()); err != nil {
		t.Fatal(err)
	}
	// Restoring an export puts another key into the settings folder.
	other, _, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := identity.Restore(settings, other.PrivateKey); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := LoadIdentity(settings, testLogger()); err != nil {
		t.Fatal(err)
	}
	if priv, ok := readKeyBackup(settings); !ok || !priv.Equal(other.PrivateKey) {
		t.Fatal("the backup did not follow the restored identity")
	}
}

func TestForgetIdentityStartsOver(t *testing.T) {
	useTempKeyBackups(t)
	settings := t.TempDir()
	first, _, _, err := LoadIdentity(settings, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	if err := ForgetIdentity(settings); err != nil {
		t.Fatal(err)
	}
	next, created, restored, err := LoadIdentity(settings, testLogger())
	if err != nil || !created || restored {
		t.Fatalf("after forgetting: created=%v restored=%v err=%v", created, restored, err)
	}
	if next.PublicKey.Equal(first.PublicKey) {
		t.Fatal("forgetting kept the old key")
	}
}

func TestBackupsAreSeparatePerSettingsFolder(t *testing.T) {
	useTempKeyBackups(t)
	a, _, _, _ := LoadIdentity(t.TempDir(), testLogger())
	b, _, _, _ := LoadIdentity(t.TempDir(), testLogger())
	if a.PublicKey.Equal(b.PublicKey) {
		t.Fatal("two settings folders share one key")
	}
}
