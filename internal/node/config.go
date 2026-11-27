// Package node implements the Link Node: identity, registration, presence
// and the local preview server around one public folder.
package node

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Config holds the node's settings, from flags or environment variables.
type Config struct {
	Server      string   // base URL of the Link.us service
	DataDir     string   // identity and state
	PublicDir   string   // the public folder
	PreviewAddr string   // loopback listen address for the local preview
	UIAddr      string   // loopback listen address for the settings page
	STUN        []string // STUN servers; empty derives them from Server
	PortMap     bool     // ask the router for a UDP port mapping
	Tray        bool     // show the tray icon where one exists
	ConfigFile  string   // the config file that was applied, if any

	// PublicDirPinned reports that PublicDir came from a flag, the
	// environment or the config file, so the saved folder choice is
	// ignored. Set by Defaults.
	PublicDirPinned bool

	// The folders that are the defaults under ~/Link (set by Defaults):
	// only these are renamed by AdoptLinkFolders.
	dataDirDefault, publicDirDefault bool
}

// Defaults fills empty fields with the documented defaults.
func (c *Config) Defaults() error {
	c.PublicDirPinned = c.PublicDir != ""
	if c.Server == "" {
		c.Server = "https://hub.link.us"
	}
	if len(c.STUN) == 0 {
		c.STUN = STUNServers(c.Server)
	}
	if c.DataDir == "" {
		dir, err := DefaultDataDir()
		if err != nil {
			return err
		}
		c.DataDir = dir
		c.dataDirDefault = true
	}
	if c.PublicDir == "" {
		// The folder picked on the settings page outlives the run; an
		// explicit --public-dir / LINKNODE_PUBLIC_DIR still wins over it.
		if saved := SavedPublicDir(c.DataDir); saved != "" {
			c.PublicDir = saved
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("resolve home dir: %w", err)
			}
			c.PublicDir = LinkFolder(home, "Public")
			c.publicDirDefault = true
		}
	}
	if c.PreviewAddr == "" {
		c.PreviewAddr = "127.0.0.1:7465"
	}
	if c.UIAddr == "" {
		c.UIAddr = "127.0.0.1:7466"
	}
	return nil
}

// CheckPublicDir refuses public folders that would expose existing personal
// files: a home directory, a drive root, Desktop, Documents or Downloads,
// and any directory containing the data directory (which holds the private
// key).
func CheckPublicDir(publicDir, dataDir string) error {
	pub, err := filepath.Abs(publicDir)
	if err != nil {
		return err
	}
	data, err := filepath.Abs(dataDir)
	if err != nil {
		return err
	}

	// Worded as refusals of a request, never as descriptions of state: the
	// same text reaches the terminal at startup and the settings page when
	// a change is declined, and in both places nothing has been applied.
	if pub == filepath.Dir(pub) {
		return fmt.Errorf("refusing to serve %s: it is a filesystem root; pick a dedicated folder such as ~/Link/Public", pub)
	}

	if home, err := os.UserHomeDir(); err == nil {
		if samePath(pub, home) {
			return fmt.Errorf("refusing to serve %s: it is your home directory, and everything in it would become public; pick a dedicated folder such as %s", pub, filepath.Join(home, "Link", "Public"))
		}
		for _, name := range []string{"Desktop", "Documents", "Downloads"} {
			if samePath(pub, filepath.Join(home, name)) {
				return fmt.Errorf("refusing to serve %s: it is your %s folder, and its existing contents would become public; pick a dedicated folder such as %s", pub, name, filepath.Join(home, "Link", "Public"))
			}
		}
	}

	if rel, err := filepath.Rel(pub, data); err == nil {
		if rel == "." || !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".." {
			return fmt.Errorf("refusing to serve %s: it contains the settings folder %s, which holds the node's private key", pub, data)
		}
	}
	return nil
}

// samePath compares cleaned absolute paths, case-insensitively on the
// case-insensitive desktop platforms.
func samePath(a, b string) bool {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// CheckPreviewAddr requires a loopback listen address, so the preview is
// never reachable from the network.
func CheckPreviewAddr(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid preview address %q: %w", addr, err)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("preview address %q is not loopback; the preview must stay local", addr)
	}
	return nil
}

// placeholderIndex is written into a newly created public folder.
const placeholderIndex = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>Your Link Node is running</title>
<style>body{font-family:system-ui,sans-serif;max-width:40rem;margin:4rem auto;padding:0 1rem;line-height:1.5}</style>
</head>
<body>
<h1>Your Link Node is running</h1>
<p>This page is the placeholder <code>index.html</code> in your public folder.
Everything you put in this folder is served as your site; replace this file
with your own homepage.</p>
<p>You can also open an AI coding agent such as Claude Code or Codex in this
folder and ask it to make you a homepage.</p>
</body>
</html>
`

// IsPlaceholderHomepage reports whether dir's index.html is still the
// untouched placeholder: a folder holding only it has no homepage yet.
func IsPlaceholderHomepage(dir string) bool {
	f, err := os.Open(filepath.Join(dir, "index.html"))
	if err != nil {
		return false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(len(placeholderIndex))+1))
	return err == nil && string(data) == placeholderIndex
}

// EnsurePublicDir creates the public folder if needed, with a placeholder
// index.html when the folder is new or empty.
func EnsurePublicDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return nil
	}
	return os.WriteFile(filepath.Join(dir, "index.html"), []byte(placeholderIndex), 0o644)
}
