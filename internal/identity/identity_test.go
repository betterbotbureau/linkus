package identity

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/betterbotbureau/linkus/internal/protocol"
)

func TestCreateLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()

	id, created, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if !created {
		t.Fatal("expected a new identity to be created")
	}
	if id.NodeID != "" {
		t.Fatalf("new identity has node ID %q, want empty", id.NodeID)
	}

	if err := id.SetNodeID("58310427"); err != nil {
		t.Fatalf("SetNodeID: %v", err)
	}

	again, created, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if created {
		t.Fatal("second load created a new identity")
	}
	if !again.PublicKey.Equal(id.PublicKey) {
		t.Error("public key changed across reload")
	}
	if again.NodeID != "58310427" {
		t.Errorf("node ID = %q, want 58310427", again.NodeID)
	}
}

func TestFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mode bits are advisory on Windows")
	}
	dir := t.TempDir()
	if _, _, err := LoadOrCreate(dir); err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, FileName))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("identity file permissions = %o, want 600", perm)
	}
}

func TestSignVerify(t *testing.T) {
	dir := t.TempDir()
	id, _, err := LoadOrCreate(dir)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}

	msg := protocol.RegisterSigningBytes(id.PublicKey)
	sig := id.Sign(msg)

	if !Verify(id.PublicKey, msg, sig) {
		t.Error("valid signature rejected")
	}

	tampered := append([]byte(nil), msg...)
	tampered[len(tampered)-1] ^= 1
	if Verify(id.PublicKey, tampered, sig) {
		t.Error("signature accepted over tampered message")
	}

	other, _, err := LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("LoadOrCreate other: %v", err)
	}
	if Verify(other.PublicKey, msg, sig) {
		t.Error("signature accepted under a different key")
	}
}

func TestCorruptFileRejected(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadOrCreate(dir); err == nil {
		t.Fatal("corrupt identity file silently replaced; want error")
	}
}
