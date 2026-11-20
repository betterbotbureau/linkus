package node

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckPublicDir(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(t.TempDir(), "data")

	refused := []struct {
		name string
		dir  string
	}{
		{"home", home},
		{"desktop", filepath.Join(home, "Desktop")},
		{"documents", filepath.Join(home, "Documents")},
		{"downloads", filepath.Join(home, "Downloads")},
		{"root", string(filepath.Separator)},
		{"data dir itself", data},
		{"ancestor of data dir", filepath.Dir(data)},
	}
	for _, tc := range refused {
		t.Run("refuses "+tc.name, func(t *testing.T) {
			if err := CheckPublicDir(tc.dir, data); err == nil {
				t.Errorf("CheckPublicDir(%q) accepted, want refusal", tc.dir)
			}
		})
	}

	allowed := []struct {
		name string
		dir  string
	}{
		{"dedicated folder", filepath.Join(home, "Link", "public")},
		{"sibling of data dir", filepath.Join(filepath.Dir(data), "public")},
		{"folder under documents", filepath.Join(home, "Documents", "site")},
	}
	for _, tc := range allowed {
		t.Run("allows "+tc.name, func(t *testing.T) {
			if err := CheckPublicDir(tc.dir, data); err != nil {
				t.Errorf("CheckPublicDir(%q) = %v, want nil", tc.dir, err)
			}
		})
	}
}

func TestCheckPreviewAddr(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:7465", "localhost:0", "[::1]:7465"} {
		if err := CheckPreviewAddr(addr); err != nil {
			t.Errorf("CheckPreviewAddr(%q) = %v, want nil", addr, err)
		}
	}
	for _, addr := range []string{"0.0.0.0:7465", "192.168.1.5:7465", "example.com:80", "no-port"} {
		if err := CheckPreviewAddr(addr); err == nil {
			t.Errorf("CheckPreviewAddr(%q) accepted, want refusal", addr)
		}
	}
}

func TestEnsurePublicDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "public")
	if err := EnsurePublicDir(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "index.html")); err != nil {
		t.Errorf("placeholder index.html not created: %v", err)
	}

	// An existing non-empty folder is left alone.
	custom := []byte("my homepage")
	if err := os.WriteFile(filepath.Join(dir, "index.html"), custom, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsurePublicDir(dir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(custom) {
		t.Error("EnsurePublicDir overwrote an existing index.html")
	}
}
