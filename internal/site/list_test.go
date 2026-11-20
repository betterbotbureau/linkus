package site

import (
	"os"
	"path/filepath"
	"testing"
)

func TestList(t *testing.T) {
	dir := t.TempDir()
	mustWrite := func(rel string, content string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("index.html", "<p>x</p>")
	mustWrite("css/s.css", "p{}")
	mustWrite(".secret", "hidden")
	mustWrite("CLAUDE.md", "instructions")
	mustWrite(".git/config", "x")
	if err := os.Symlink(filepath.Join(dir, "index.html"), filepath.Join(dir, "link.html")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}

	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	served, skipped, err := st.List()
	if err != nil {
		t.Fatal(err)
	}

	wantServed := []string{"/css/s.css", "/index.html"}
	if len(served) != len(wantServed) {
		t.Fatalf("served = %+v, want %v", served, wantServed)
	}
	for i, w := range wantServed {
		if served[i].Path != w {
			t.Fatalf("served[%d] = %q, want %q", i, served[i].Path, w)
		}
	}
	reasons := map[string]string{}
	for _, s := range skipped {
		reasons[s.Path] = s.Reason
	}
	if reasons["/.secret"] != "dotfile" || reasons["/CLAUDE.md"] != "agent instruction file" ||
		reasons["/.git"] != "dotfile" || reasons["/link.html"] != "not a regular file" {
		t.Fatalf("skipped = %v", reasons)
	}
	if _, ok := reasons["/.git/config"]; ok {
		t.Fatal("walked into a skipped directory")
	}
}
