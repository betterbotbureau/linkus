package node

import (
	"flag"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseConfigFile(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    map[string]string
		wantErr string
	}{
		{name: "empty", in: "", want: map[string]string{}},
		{
			name: "comments blanks and spacing",
			in:   "# my node\n\n  server = https://example.net  \npublic-dir=/srv/site\n",
			want: map[string]string{"server": "https://example.net", "public-dir": "/srv/site"},
		},
		{
			name: "quoted value with spaces and a BOM",
			in:   "\uFEFFpublic-dir = \"/srv/my site\"\r\nportmap = false\r\n",
			want: map[string]string{"public-dir": "/srv/my site", "portmap": "false"},
		},
		{name: "keys are case-insensitive", in: "Server = x\n", want: map[string]string{"server": "x"}},
		{name: "equals inside value", in: "server = https://h/?a=b\n", want: map[string]string{"server": "https://h/?a=b"}},
		{name: "unknown key", in: "pubic-dir = /x\n", wantErr: `line 1: unknown setting "pubic-dir"`},
		{name: "duplicate key", in: "portmap = false\n# again\nportmap = true\n", wantErr: `line 3: "portmap" already set on line 1`},
		{name: "missing equals", in: "server https://x\n", wantErr: "line 1: expected key = value"},
		{name: "empty value", in: "server =\n", wantErr: `line 1: "server" has no value`},
		{name: "config key itself is not a setting", in: "config = /etc/x\n", wantErr: "unknown setting"},
		{name: "oversized", in: strings.Repeat("#", maxConfigFile+1), wantErr: "larger than"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseConfigFile(strings.NewReader(tt.in))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			m := map[string]string{}
			for _, e := range got {
				m[e.Key] = e.Value
			}
			if len(m) != len(tt.want) {
				t.Fatalf("got %v, want %v", m, tt.want)
			}
			for k, v := range tt.want {
				if m[k] != v {
					t.Fatalf("%s = %q, want %q", k, m[k], v)
				}
			}
		})
	}
}

// testFlags mirrors the run flags that matter for precedence.
func testFlags() (*flag.FlagSet, *string, *string, *string, *bool) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	server := fs.String("server", os.Getenv("LINKNODE_SERVER"), "")
	pub := fs.String("public-dir", "", "")
	data := fs.String("data-dir", "", "")
	pm := fs.Bool("portmap", true, "")
	for _, k := range []string{"preview-addr", "stun"} {
		fs.String(k, "", "")
	}
	return fs, server, pub, data, pm
}

func writeConf(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), ConfigFileName)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestApplyConfigFilePrecedence(t *testing.T) {
	t.Setenv("LINKNODE_SERVER", "")
	path := writeConf(t, "server = https://from-file\npublic-dir = /from/file\nportmap = false\n")

	// The file fills unset settings.
	fs, server, pub, _, pm := testFlags()
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if err := ApplyConfigFile(fs, path, false); err != nil {
		t.Fatal(err)
	}
	if *server != "https://from-file" || *pub != "/from/file" || *pm {
		t.Fatalf("file not applied: server=%q pub=%q portmap=%v", *server, *pub, *pm)
	}

	// An explicit flag wins over the file.
	fs, server, pub, _, _ = testFlags()
	if err := fs.Parse([]string{"--public-dir", "/from/flag"}); err != nil {
		t.Fatal(err)
	}
	if err := ApplyConfigFile(fs, path, false); err != nil {
		t.Fatal(err)
	}
	if *pub != "/from/flag" || *server != "https://from-file" {
		t.Fatalf("flag should win: pub=%q server=%q", *pub, *server)
	}

	// The environment wins over the file.
	t.Setenv("LINKNODE_SERVER", "https://from-env")
	fs, server, _, _, _ = testFlags()
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	if err := ApplyConfigFile(fs, path, false); err != nil {
		t.Fatal(err)
	}
	if *server != "https://from-env" {
		t.Fatalf("env should win: server=%q", *server)
	}
}

func TestApplyConfigFileRefusals(t *testing.T) {
	t.Setenv("LINKNODE_DATA_DIR", "")

	// A file inside the data directory cannot move the data directory.
	fs, _, _, _, _ := testFlags()
	_ = fs.Parse(nil)
	err := ApplyConfigFile(fs, writeConf(t, "data-dir = /elsewhere\n"), true)
	if err == nil || !strings.Contains(err.Error(), "--config") {
		t.Fatalf("data-dir in the data-dir file: err = %v", err)
	}
	// ...but a file named with --config can.
	fs, _, _, data, _ := testFlags()
	_ = fs.Parse(nil)
	if err := ApplyConfigFile(fs, writeConf(t, "data-dir = /elsewhere\n"), false); err != nil || *data != "/elsewhere" {
		t.Fatalf("data-dir via --config: err=%v data=%q", err, *data)
	}

	// Bad values name the line.
	fs, _, _, _, _ = testFlags()
	_ = fs.Parse(nil)
	err = ApplyConfigFile(fs, writeConf(t, "# x\nportmap = maybe\n"), false)
	if err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("bad bool: err = %v", err)
	}

	// A missing named file is an error, not a silent default.
	fs, _, _, _, _ = testFlags()
	_ = fs.Parse(nil)
	if err := ApplyConfigFile(fs, filepath.Join(t.TempDir(), "nope.conf"), false); err == nil {
		t.Fatal("missing config file accepted")
	}
}

func TestApplyConfigFileExpandsHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory")
	}
	t.Setenv("LINKNODE_PUBLIC_DIR", "")
	fs, _, pub, _, _ := testFlags()
	_ = fs.Parse(nil)
	if err := ApplyConfigFile(fs, writeConf(t, "public-dir = ~/Link/site\n"), false); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, "Link", "site"); *pub != want {
		t.Fatalf("public-dir = %q, want %q", *pub, want)
	}
}

func TestDefaultsMarksPinnedPublicDir(t *testing.T) {
	dataDir := t.TempDir()
	if err := SavePublicDir(dataDir, "/saved/choice"); err != nil {
		t.Fatal(err)
	}
	c := Config{DataDir: dataDir}
	if err := c.Defaults(); err != nil {
		t.Fatal(err)
	}
	if c.PublicDirPinned || c.PublicDir != "/saved/choice" {
		t.Fatalf("saved choice: pinned=%v dir=%q", c.PublicDirPinned, c.PublicDir)
	}
	c = Config{DataDir: dataDir, PublicDir: "/explicit"}
	if err := c.Defaults(); err != nil {
		t.Fatal(err)
	}
	if !c.PublicDirPinned || c.PublicDir != "/explicit" {
		t.Fatalf("explicit: pinned=%v dir=%q", c.PublicDirPinned, c.PublicDir)
	}
}

// oldHome makes a home with a v2.05-style ~/Link/settings (holding an
// identity) and ~/Link/public, and points $HOME at it.
func oldHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, d := range []string{"settings", "public"} {
		if err := os.MkdirAll(filepath.Join(home, "Link", d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "Link", "Settings")); err == nil {
		t.Skip("this file system ignores case")
	}
	os.WriteFile(filepath.Join(home, "Link", "settings", "identity.json"), []byte("{}"), 0o600) //nolint:errcheck
	os.WriteFile(filepath.Join(home, "Link", "public", "index.html"), []byte("hi"), 0o644)      //nolint:errcheck
	return home
}

// A free loopback address for the preview, so no running node is seen.
func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	return addr
}

// Until the node starts, commands use the old lowercase folders as they
// are; the starting node renames them, so an upgrade keeps its identity
// and site where case matters (Linux).
func TestLinkFoldersAdoptedAtStart(t *testing.T) {
	home := oldHome(t)
	c := Config{PreviewAddr: freeAddr(t)}
	if err := c.Defaults(); err != nil {
		t.Fatal(err)
	}
	if c.DataDir != filepath.Join(home, "Link", "settings") || c.PublicDir != filepath.Join(home, "Link", "public") {
		t.Fatalf("before start: %s %s", c.DataDir, c.PublicDir)
	}
	AdoptLinkFolders(&c)
	if c.DataDir != filepath.Join(home, "Link", "Settings") || c.PublicDir != filepath.Join(home, "Link", "Public") {
		t.Fatalf("after start: %s %s", c.DataDir, c.PublicDir)
	}
	for _, f := range []string{"Settings/identity.json", "Public/index.html"} {
		if _, err := os.Stat(filepath.Join(home, "Link", f)); err != nil {
			t.Fatalf("%s not moved: %v", f, err)
		}
	}
	// The next start finds the new folders.
	c2 := Config{}
	if err := c2.Defaults(); err != nil || c2.DataDir != filepath.Join(home, "Link", "Settings") {
		t.Fatalf("next start: %s %v", c2.DataDir, err)
	}
}

func TestLinkFoldersNotMoved(t *testing.T) {
	t.Run("a node runs", func(t *testing.T) {
		home := oldHome(t)
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer ln.Close()
		c := Config{PreviewAddr: ln.Addr().String()}
		c.Defaults() //nolint:errcheck
		AdoptLinkFolders(&c)
		if c.DataDir != filepath.Join(home, "Link", "settings") {
			t.Fatalf("moved under a running node: %s", c.DataDir)
		}
	})
	t.Run("chosen folders", func(t *testing.T) {
		home := oldHome(t)
		c := Config{PreviewAddr: freeAddr(t), DataDir: filepath.Join(home, "Link", "settings"), PublicDir: filepath.Join(home, "Link", "public")}
		c.Defaults() //nolint:errcheck
		AdoptLinkFolders(&c)
		if _, err := os.Stat(filepath.Join(home, "Link", "settings")); err != nil {
			t.Fatal("a chosen settings folder was moved")
		}
		if _, err := os.Stat(filepath.Join(home, "Link", "public")); err != nil {
			t.Fatal("a chosen public folder was moved")
		}
	})
	t.Run("both exist", func(t *testing.T) {
		home := oldHome(t)
		os.MkdirAll(filepath.Join(home, "Link", "Public"), 0o755) //nolint:errcheck
		c := Config{PreviewAddr: freeAddr(t)}
		c.Defaults() //nolint:errcheck
		AdoptLinkFolders(&c)
		if c.PublicDir != filepath.Join(home, "Link", "Public") {
			t.Fatalf("public: %s", c.PublicDir)
		}
		if _, err := os.Stat(filepath.Join(home, "Link", "public", "index.html")); err != nil {
			t.Fatal("old folder touched although the new one exists")
		}
	})
	t.Run("a file, and a link", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		link := filepath.Join(home, "Link")
		os.MkdirAll(link, 0o755)                                          //nolint:errcheck
		os.WriteFile(filepath.Join(link, "settings"), []byte("x"), 0o600) //nolint:errcheck
		if _, err := os.Stat(filepath.Join(link, "Settings")); err == nil {
			t.Skip("this file system ignores case")
		}
		elsewhere := t.TempDir()
		if err := os.Symlink(elsewhere, filepath.Join(link, "public")); err != nil {
			t.Skip("no symlinks here")
		}
		c := Config{PreviewAddr: freeAddr(t)}
		c.Defaults() //nolint:errcheck
		AdoptLinkFolders(&c)
		if c.DataDir != filepath.Join(link, "Settings") {
			t.Fatalf("a file counted as the settings folder: %s", c.DataDir)
		}
		if target, err := os.Readlink(filepath.Join(link, "Public")); err != nil || target != elsewhere {
			t.Fatalf("link not renamed as a link: %q %v", target, err)
		}
	})
}
