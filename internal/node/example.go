package node

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/betterbotbureau/linkus/web"
)

// ErrHasHomepage refuses the example site over an existing index.html.
var ErrHasHomepage = errors.New("the folder already has an index.html")

// WriteExampleSite writes the built-in example site (web/starter, a
// personal homepage) into dir, once: an existing index.html means the
// owner already has a homepage — unless it is the untouched placeholder,
// which the example replaces — and no other file that already exists is
// ever overwritten. The example's README stays in the repository only.
func WriteExampleSite(dir string) error {
	index := filepath.Join(dir, "index.html")
	if IsPlaceholderHomepage(dir) {
		if err := os.Remove(index); err != nil {
			return err
		}
	} else if _, err := os.Stat(index); err == nil {
		return ErrHasHomepage
	}
	starter, err := fs.Sub(web.StarterFS, "starter")
	if err != nil {
		return err
	}
	return fs.WalkDir(starter, ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || path == "README.md" {
			return nil
		}
		dst := filepath.Join(dir, filepath.FromSlash(path))
		if _, err := os.Stat(dst); err == nil {
			return nil // the owner's file wins
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		data, err := fs.ReadFile(starter, path)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
}
