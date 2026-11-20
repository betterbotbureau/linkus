package node

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFolderSwap(t *testing.T) {
	dataDir := t.TempDir()
	dirA := filepath.Join(t.TempDir(), "a")
	dirB := filepath.Join(t.TempDir(), "b")
	for dir, body := range map[string]string{dirA: "site A", dirB: "site B"} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	f, err := OpenPublicFolder(dirA, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	read := func() string {
		t.Helper()
		file, err := f.Open("/index.html")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		data, _ := io.ReadAll(file)
		return string(data)
	}
	if got := read(); got != "site A" || f.Dir() != dirA {
		t.Fatalf("before swap: %q from %s", got, f.Dir())
	}

	// A bad target changes nothing: the folder must not contain the key.
	if err := f.Swap(filepath.Dir(dataDir)); err == nil {
		t.Fatal("swap into a folder containing the data dir should fail")
	}
	if f.Dir() != dirA || read() != "site A" {
		t.Fatalf("failed swap moved the folder: %s", f.Dir())
	}

	if err := f.Swap(dirB); err != nil {
		t.Fatal(err)
	}
	if got := read(); got != "site B" || f.Dir() != dirB {
		t.Fatalf("after swap: %q from %s", got, f.Dir())
	}
	if saved := SavedPublicDir(dataDir); saved != dirB {
		t.Fatalf("choice not persisted: %q", saved)
	}

	// A missing target is created, placeholder and all.
	dirC := filepath.Join(t.TempDir(), "brand", "new")
	if err := f.Swap(dirC); err != nil {
		t.Fatal(err)
	}
	if got := read(); !strings.Contains(got, "placeholder") {
		t.Fatalf("new folder has no placeholder: %q", got)
	}

	// Swapping to the current folder is a quiet no-op.
	if err := f.Swap(dirC); err != nil {
		t.Fatal(err)
	}
}

func TestSavedPublicDirWinsOverDefault(t *testing.T) {
	dataDir := t.TempDir()
	chosen := filepath.Join(t.TempDir(), "chosen")
	if err := SavePublicDir(dataDir, chosen); err != nil {
		t.Fatal(err)
	}
	cfg := Config{DataDir: dataDir}
	if err := cfg.Defaults(); err != nil {
		t.Fatal(err)
	}
	if cfg.PublicDir != chosen {
		t.Fatalf("Defaults ignored the saved folder: %q", cfg.PublicDir)
	}
	// An explicit flag wins over the saved choice.
	explicit := Config{DataDir: dataDir, PublicDir: "/somewhere/else"}
	if err := explicit.Defaults(); err != nil {
		t.Fatal(err)
	}
	if explicit.PublicDir != "/somewhere/else" {
		t.Fatalf("explicit folder overridden: %q", explicit.PublicDir)
	}
}
