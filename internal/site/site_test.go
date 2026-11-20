package site

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// newTestSite builds a public folder with safe and hostile contents.
func newTestSite(t *testing.T) *Site {
	t.Helper()
	dir := t.TempDir()

	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("index.html", "home")
	write("page.html", "page")
	write("css/style.css", "body{}")
	write("sub/index.html", "sub home")
	write("noindex/file.txt", "no index here")
	write(".hidden", "secret")
	write(".well-known/token", "secret")
	write("AGENTS.md", "instructions")
	write("CLAUDE.md", "instructions")
	write("docs/agents.md", "instructions, lower case, nested")

	// A file outside the public folder that symlinks will point at.
	outside := filepath.Join(filepath.Dir(dir), "outside-"+filepath.Base(dir))
	if err := os.WriteFile(outside, []byte("outside"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(outside) })

	if runtime.GOOS != "windows" {
		if err := os.Symlink(outside, filepath.Join(dir, "link-out.html")); err != nil {
			t.Fatal(err)
		}
		// Relative target inside the root. (os.Root refuses absolute
		// symlink targets even when they point inside the folder.)
		if err := os.Symlink("page.html", filepath.Join(dir, "link-in.html")); err != nil {
			t.Fatal(err)
		}
	}

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open site: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpen(t *testing.T) {
	s := newTestSite(t)

	serves := func(path, want string) func(*testing.T) {
		return func(t *testing.T) {
			f, err := s.Open(path)
			if err != nil {
				t.Fatalf("Open(%q): %v", path, err)
			}
			defer f.Close()
			data, err := io.ReadAll(f)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != want {
				t.Errorf("Open(%q) = %q, want %q", path, data, want)
			}
		}
	}
	notFound := func(path string) func(*testing.T) {
		return func(t *testing.T) {
			f, err := s.Open(path)
			if err == nil {
				f.Close()
				t.Fatalf("Open(%q) succeeded, want ErrNotFound", path)
			}
			if !errors.Is(err, ErrNotFound) {
				t.Errorf("Open(%q) = %v, want ErrNotFound", path, err)
			}
		}
	}

	// Served.
	t.Run("root serves index", serves("/", "home"))
	t.Run("plain file", serves("/page.html", "page"))
	t.Run("nested file", serves("/css/style.css", "body{}"))
	t.Run("directory with index", serves("/sub/", "sub home"))
	t.Run("directory without trailing slash", serves("/sub", "sub home"))

	// Traversal and malformed paths.
	t.Run("dotdot", notFound("/../secret"))
	t.Run("dotdot inside", notFound("/css/../../secret"))
	t.Run("decoded encoded traversal", notFound("/../../etc/passwd"))
	t.Run("lone dot segment", notFound("/./page.html"))
	t.Run("backslash", notFound("/css\\style.css"))
	t.Run("backslash traversal", notFound("/..\\..\\secret"))
	t.Run("null byte", notFound("/page.html\x00.jpg"))
	t.Run("not absolute", notFound("page.html"))
	t.Run("windows absolute", notFound("C:/Windows/system.ini"))
	t.Run("empty segment", notFound("//etc/passwd"))
	t.Run("very long path", notFound("/"+strings.Repeat("a/", 4096)+"x"))
	t.Run("very long segment", notFound("/"+strings.Repeat("a", 300)))

	// Hidden names: what the owner cannot keep out of the folder —
	// dotfiles and the agent instruction files.
	t.Run("dotfile", notFound("/.hidden"))
	t.Run("dot directory", notFound("/.well-known/token"))
	t.Run("AGENTS.md", notFound("/AGENTS.md"))
	t.Run("CLAUDE.md", notFound("/CLAUDE.md"))
	t.Run("agents.md case-insensitive nested", notFound("/docs/agents.md"))

	// Directories and misses.
	t.Run("directory without index", notFound("/noindex/"))
	t.Run("missing file", notFound("/nope.html"))

	if runtime.GOOS != "windows" {
		t.Run("symlink out of root", notFound("/link-out.html"))
		t.Run("symlink inside root", serves("/link-in.html", "page"))
	}
}

func TestHandler(t *testing.T) {
	h := Handler(newTestSite(t))

	get := func(target string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		return rec
	}

	if rec := get("/"); rec.Code != http.StatusOK || rec.Body.String() != "home" {
		t.Errorf("GET / = %d %q, want 200 home", rec.Code, rec.Body.String())
	}

	rec := get("/css/style.css")
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/css") {
		t.Errorf("css Content-Type = %q", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}

	// Percent-encoded traversal arriving over real HTTP decoding.
	for _, target := range []string{
		"/%2e%2e/%2e%2e/etc/passwd",
		"/%2e%2e%2f%2e%2e%2fetc%2fpasswd",
		"/..%2f..%2fetc%2fpasswd",
		"/AGENTS.md",
		"/.hidden",
	} {
		if rec := get(target); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", target, rec.Code)
		}
	}

	// Methods other than GET and HEAD.
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(method, "/", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s / = %d, want 405", method, rec.Code)
		}
	}

	// HEAD works and carries no body.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodHead, "/page.html", nil))
	if rec.Code != http.StatusOK || rec.Body.Len() != 0 {
		t.Errorf("HEAD /page.html = %d with %d body bytes, want 200 and none", rec.Code, rec.Body.Len())
	}
}
