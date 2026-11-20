package node

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/betterbotbureau/linkus/internal/site"
)

// Folder is the public folder as a live handle the owner can re-point from
// the settings page. Everything that serves the folder — the WebRTC fetch
// channels, the local preview, the settings page — goes through it, so a
// swap takes effect everywhere at once. The choice persists in the data
// directory and wins over the built-in default on the next start (an
// explicit --public-dir flag still wins over both).

const publicDirFile = "public_dir"

type folderState struct {
	dir  string
	site *site.Site
}

// Folder is safe for concurrent use.
type Folder struct {
	dataDir string
	swapMu  sync.Mutex // serializes swaps; reads go through the pointer
	cur     atomic.Pointer[folderState]
	// conflict refuses folders that would overlap the Inbox; may be nil.
	conflict func(dir string) error
}

// SetConflictCheck adds a refusal to every later Swap (the Inbox's
// overlap check). Call it before the settings page is served.
func (f *Folder) SetConflictCheck(check func(dir string) error) { f.conflict = check }

// OpenPublicFolder opens dir as the current public folder. dir must
// already be validated (CheckPublicDir) and existing (EnsurePublicDir).
func OpenPublicFolder(dir, dataDir string) (*Folder, error) {
	s, err := site.Open(dir)
	if err != nil {
		return nil, err
	}
	f := &Folder{dataDir: dataDir}
	f.cur.Store(&folderState{dir: dir, site: s})
	return f, nil
}

// Dir returns the current folder path.
func (f *Folder) Dir() string { return f.cur.Load().dir }

// Open resolves a request path in the current folder (site.Opener).
func (f *Folder) Open(path string) (*site.File, error) {
	return f.cur.Load().site.Open(path)
}

// List lists the current folder the way visitors see it.
func (f *Folder) List() (served []site.Entry, skipped []site.Skipped, err error) {
	return f.cur.Load().site.List()
}

// Swap re-points the folder at newDir: validated, created if missing,
// opened, then switched atomically and persisted. On any error the old
// folder stays in place. Fetches running on the old handle finish on
// their already-open files.
func (f *Folder) Swap(newDir string) error {
	f.swapMu.Lock()
	defer f.swapMu.Unlock()
	abs, err := filepath.Abs(strings.TrimSpace(newDir))
	if err != nil {
		return err
	}
	if samePath(abs, f.Dir()) {
		return nil
	}
	if err := CheckPublicDir(abs, f.dataDir); err != nil {
		return err
	}
	if f.conflict != nil {
		if err := f.conflict(abs); err != nil {
			return err
		}
	}
	if err := EnsurePublicDir(abs); err != nil {
		return err
	}
	s, err := site.Open(abs)
	if err != nil {
		return err
	}
	old := f.cur.Swap(&folderState{dir: abs, site: s})
	if err := SavePublicDir(f.dataDir, abs); err != nil {
		// The swap already happened; the choice just won't survive a
		// restart. Report it rather than unwinding.
		old.site.Close()
		return err
	}
	return old.site.Close()
}

// Close releases the current folder handle.
func (f *Folder) Close() error { return f.cur.Load().site.Close() }

// SavePublicDir records the owner's folder choice in the data directory.
func SavePublicDir(dataDir, dir string) error {
	return os.WriteFile(filepath.Join(dataDir, publicDirFile), []byte(dir+"\n"), 0o600)
}

// SavedPublicDir returns the recorded choice, or "" when none exists.
func SavedPublicDir(dataDir string) string {
	data, err := os.ReadFile(filepath.Join(dataDir, publicDirFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
