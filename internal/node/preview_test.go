package node

import (
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The preview explains a missing homepage to the owner; everything else
// behaves exactly like the visitor-facing serving rules.
func TestPreviewExplainsMissingIndex(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := OpenPublicFolder(dir, filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	p, err := StartPreview("127.0.0.1:0", f, log)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	get := func(path string) (int, string) {
		res, err := http.Get("http://" + p.Addr + path)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(body)
	}

	code, body := get("/")
	if code != http.StatusNotFound {
		t.Fatalf("/ without index.html = %d, want 404", code)
	}
	if !strings.Contains(body, "index.html") || !strings.Contains(body, dir) {
		t.Fatalf("notice does not explain the missing index: %q", body)
	}
	if code, body := get("/hello.txt"); code != http.StatusOK || body != "hi" {
		t.Fatalf("/hello.txt = %d %q, want 200 hi", code, body)
	}
	// Non-root misses stay plain 404s.
	if _, body := get("/nope/"); strings.Contains(body, "index.html") {
		t.Fatalf("non-root 404 carries the notice: %q", body)
	}

	// With an index.html the homepage serves normally again.
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("home"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, body := get("/"); code != http.StatusOK || body != "home" {
		t.Fatalf("/ with index.html = %d %q, want 200 home", code, body)
	}
}
