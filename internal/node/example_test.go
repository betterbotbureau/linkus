package node

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteExampleSite(t *testing.T) {
	// A freshly created folder holds the placeholder; the example replaces it.
	dir := filepath.Join(t.TempDir(), "public")
	if err := EnsurePublicDir(dir); err != nil {
		t.Fatal(err)
	}
	if !IsPlaceholderHomepage(dir) {
		t.Fatal("new folder's index.html not recognised as the placeholder")
	}
	if err := WriteExampleSite(dir); err != nil {
		t.Fatalf("example over the placeholder: %v", err)
	}
	index, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil || !strings.Contains(string(index), "Alex's Place") {
		t.Fatalf("example homepage not written over the placeholder: %v", err)
	}
	if IsPlaceholderHomepage(dir) {
		t.Fatal("example homepage still reported as the placeholder")
	}

	// A second run finds a real homepage and refuses.
	if err := WriteExampleSite(dir); !errors.Is(err, ErrHasHomepage) {
		t.Fatalf("second run: err = %v, want ErrHasHomepage", err)
	}

	// An owner-edited index.html is never replaced, even if it began as
	// the placeholder.
	edited := filepath.Join(t.TempDir(), "public")
	if err := EnsurePublicDir(edited); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(edited, "index.html"), []byte(placeholderIndex+"<p>mine</p>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if IsPlaceholderHomepage(edited) {
		t.Fatal("edited homepage reported as the placeholder")
	}
	if err := WriteExampleSite(edited); !errors.Is(err, ErrHasHomepage) {
		t.Fatalf("edited homepage: err = %v, want ErrHasHomepage", err)
	}
}
