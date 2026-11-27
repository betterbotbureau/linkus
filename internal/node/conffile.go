package node

import (
	"bufio"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ConfigFileName is the config file a node reads from its data directory
// when no --config / LINKNODE_CONFIG names one.
const ConfigFileName = "linknode.conf"

// maxConfigFile bounds what the parser reads; a real config is a few
// hundred bytes.
const maxConfigFile = 64 << 10

// ConfigKeys are the settings a config file may hold: the run flags, by
// the same names. Each also has an environment variable, LINKNODE_ plus
// the upper-cased key with '-' as '_'.
var ConfigKeys = []string{
	"server", "data-dir", "public-dir", "preview-addr", "stun", "portmap",
}

// ConfigEnv returns the environment variable for a config key.
func ConfigEnv(key string) string {
	return "LINKNODE_" + strings.ToUpper(strings.ReplaceAll(key, "-", "_"))
}

// ConfigEntry is one setting from a config file.
type ConfigEntry struct {
	Key, Value string
	Line       int
}

// ParseConfigFile reads `key = value` lines. Blank lines and lines
// starting with '#' are ignored; one pair of surrounding double quotes is
// stripped from a value, so paths with spaces may be quoted. Unknown and
// repeated keys are errors — a misspelled setting must not be silently
// ignored on a headless device.
func ParseConfigFile(r io.Reader) ([]ConfigEntry, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxConfigFile+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxConfigFile {
		return nil, fmt.Errorf("config file larger than %d KB", maxConfigFile>>10)
	}
	known := make(map[string]bool, len(ConfigKeys))
	for _, k := range ConfigKeys {
		known[k] = true
	}
	seen := map[string]int{}
	var out []ConfigEntry
	sc := bufio.NewScanner(bytes.NewReader(data))
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if n == 1 {
			line = strings.TrimPrefix(line, "\uFEFF") // Notepad's UTF-8 BOM
		}
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return nil, fmt.Errorf("line %d: expected key = value", n)
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
			value = value[1 : len(value)-1]
		}
		if !known[key] {
			return nil, fmt.Errorf("line %d: unknown setting %q (known: %s)", n, key, strings.Join(ConfigKeys, ", "))
		}
		if prev, dup := seen[key]; dup {
			return nil, fmt.Errorf("line %d: %q already set on line %d", n, key, prev)
		}
		if value == "" {
			return nil, fmt.Errorf("line %d: %q has no value", n, key)
		}
		seen[key] = n
		out = append(out, ConfigEntry{Key: key, Value: value, Line: n})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ApplyConfigFile loads path and sets each of its settings on fs, unless
// the same setting was given as a flag or through its environment
// variable: flags win over the environment, which wins over the file.
// fromDataDir marks a file found in the data directory, which cannot
// move the data directory itself.
func ApplyConfigFile(fs *flag.FlagSet, path string, fromDataDir bool) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	entries, err := ParseConfigFile(f)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	explicit := map[string]bool{}
	fs.Visit(func(fl *flag.Flag) { explicit[fl.Name] = true })
	for _, e := range entries {
		if e.Key == "data-dir" && fromDataDir {
			return fmt.Errorf("%s line %d: data-dir cannot be set in the config file inside the settings folder; pass the file with --config instead", path, e.Line)
		}
		if explicit[e.Key] || os.Getenv(ConfigEnv(e.Key)) != "" {
			continue
		}
		if e.Key == "public-dir" || e.Key == "data-dir" {
			e.Value = expandHome(e.Value)
		}
		if err := fs.Set(e.Key, e.Value); err != nil {
			return fmt.Errorf("%s line %d: %s: %w", path, e.Line, e.Key, err)
		}
	}
	return nil
}

// expandHome turns a leading ~/ into the home directory: config files are
// written by hand, and shells are not there to expand it.
func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~"))
}

// DefaultDataDir is the settings folder used when none is configured:
// ~/Link/Settings, next to the website folder ~/Link/Public, so
// everything that is the owner's Link lives in one place they can find.
func DefaultDataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return LinkFolder(home, "Settings"), nil
}

// LinkFolder is ~/Link/<name>. Releases up to v2.05 used lowercase names
// (~/Link/settings, ~/Link/public); where case matters (Linux), those
// are other folders, so while only the old one exists it is the one in
// use. AdoptLinkFolders renames it when the node starts.
func LinkFolder(home, name string) string {
	dir := filepath.Join(home, "Link", name)
	if old, ok := oldLinkFolder(home, name); ok {
		return old
	}
	return dir
}

// oldLinkFolder reports the lowercase folder when it exists and the
// capitalised one does not (a system that ignores case finds both).
func oldLinkFolder(home, name string) (string, bool) {
	dir := filepath.Join(home, "Link", name)
	old := filepath.Join(home, "Link", strings.ToLower(name))
	if old == dir {
		return "", false
	}
	if _, err := os.Lstat(dir); !errors.Is(err, fs.ErrNotExist) {
		return "", false
	}
	if fi, err := os.Stat(old); err != nil || !fi.IsDir() {
		return "", false // not a folder (a link to one counts as one)
	}
	return old, true
}

// AdoptLinkFolders renames an old lowercase default folder to its
// capitalised name. Only a starting node calls it, and it does nothing
// while another node answers on the preview address, so no running node
// has its folder moved away; a folder chosen by flag, environment,
// config file or the settings page is never moved. If a rename fails,
// the old folder stays in use.
func AdoptLinkFolders(c *Config) {
	if !c.dataDirDefault && !c.publicDirDefault {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	if c.PreviewAddr != "" && c.PreviewAddr != "off" {
		if conn, err := net.DialTimeout("tcp", c.PreviewAddr, 500*time.Millisecond); err == nil {
			conn.Close()
			return // another node runs
		}
	}
	adopt := func(dir *string, name string) {
		old, ok := oldLinkFolder(home, name)
		if !ok || *dir != old {
			return
		}
		dst := filepath.Join(home, "Link", name)
		if os.Rename(old, dst) == nil {
			*dir = dst
		}
	}
	if c.dataDirDefault {
		adopt(&c.DataDir, "Settings")
	}
	if c.publicDirDefault {
		adopt(&c.PublicDir, "Public")
	}
}

// legacyDataDir is where releases up to v1.3 kept the settings folder:
// the system's per-user application folder.
func legacyDataDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "LinkNode"), nil
}

// MigrateDataDir moves a node's settings from the pre-v1.4 location to the
// default ~/Link/Settings, once: only when the old folder holds an
// identity and the new one does not. It returns the old folder when it
// moved something. The old folder is renamed rather than deleted, so
// nothing is lost if anything goes wrong.
func MigrateDataDir() (string, error) {
	legacy, err := legacyDataDir()
	if err != nil {
		return "", nil
	}
	target, err := DefaultDataDir()
	if err != nil {
		return "", nil
	}
	return migrateDataDir(legacy, target)
}

func migrateDataDir(legacy, target string) (string, error) {
	if _, err := os.Stat(filepath.Join(legacy, "identity.json")); err != nil {
		return "", nil // nothing to move
	}
	if _, err := os.Stat(filepath.Join(target, "identity.json")); err == nil {
		return "", nil // already there
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	// A plain rename when the new folder does not exist yet (the usual
	// case, same disk); otherwise copy the files across.
	if _, err := os.Stat(target); errors.Is(err, fs.ErrNotExist) {
		if os.Rename(legacy, target) == nil {
			return legacy, nil
		}
	}
	if err := copyFiles(legacy, target); err != nil {
		return "", fmt.Errorf("move settings from %s to %s: %w", legacy, target, err)
	}
	if err := os.Rename(legacy, legacy+"-moved-to-Link-settings"); err != nil {
		return "", fmt.Errorf("settings copied to %s, but the old folder %s could not be renamed: %w", target, legacy, err)
	}
	return legacy, nil
}

// copyFiles copies the regular files of src (not subfolders) into dst,
// keeping any file dst already has.
func copyFiles(src, dst string) error {
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		to := filepath.Join(dst, e.Name())
		if _, err := os.Stat(to); err == nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			return err
		}
		if err := os.WriteFile(to, data, 0o600); err != nil {
			return err
		}
	}
	return nil
}
