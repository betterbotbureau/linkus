// Package site serves one public folder as static files and nothing else.
// The core (Site.Open) takes a request path and returns a file or an error,
// independent of net/http, so the same requests can also be answered
// arriving over a WebRTC data channel.
//
// Everything that is not a plainly safe request for a regular file inside
// the folder resolves to ErrNotFound: traversal in any encoding, absolute
// paths, backslashes, null bytes, dotfiles, the agent instruction files,
// symlinks leaving the folder, directories without an index.html. Visitors
// learn nothing about why.
//
// Hidden is exactly what the owner cannot keep out of the folder:
// dotfiles, because tools create .git or .env in a working
// folder without the owner noticing, and AGENTS.md/CLAUDE.md, because the
// node itself writes them back when they are deleted. Everything the owner
// chose to put there is public by definition.
package site

import (
	"errors"
	"io"
	"os"
	"strings"
	"time"
)

// ErrNotFound is returned for every request that does not resolve to a
// servable regular file.
var ErrNotFound = errors.New("not found")

// Opener resolves request paths to servable files: a *Site, or a handle
// that can be re-pointed at another folder while serving.
type Opener interface {
	Open(requestPath string) (*File, error)
}

const (
	indexFile = "index.html"
	// maxPathLength bounds the whole request path; maxSegmentLength bounds
	// one path element (common filesystem limit).
	maxPathLength    = 4096
	maxSegmentLength = 255
)

// neverServe lists file names that are hidden from visitors regardless of
// location, compared case-insensitively because the common desktop
// filesystems are case-insensitive.
var neverServe = []string{"agents.md", "claude.md"}

// Site is an open public folder.
type Site struct {
	root *os.Root
}

// Open opens the public folder. The folder must exist.
func Open(dir string) (*Site, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &Site{root: root}, nil
}

// Close releases the folder handle.
func (s *Site) Close() error { return s.root.Close() }

// File is a servable regular file.
type File struct {
	io.ReadSeekCloser
	Name    string // cleaned path inside the folder, for Content-Type
	Size    int64
	ModTime time.Time
}

// Open resolves a decoded request path ("/", "/css/style.css") to a file.
// A directory path resolves to its index.html. Any unsafe or unresolvable
// path returns ErrNotFound.
func (s *Site) Open(requestPath string) (*File, error) {
	rel, ok := cleanPath(requestPath)
	if !ok {
		return nil, ErrNotFound
	}
	f, err := s.open(rel)
	if err != nil {
		return nil, err
	}
	return f, nil
}

// open opens rel (a cleaned, slash-separated relative path; "" is the folder
// itself), serving index.html for directories. os.Root guarantees the open
// cannot escape the folder, including through symlinks.
func (s *Site) open(rel string) (*File, error) {
	if rel == "" {
		return s.openFile(indexFile)
	}
	f, err := s.openFile(rel)
	if err == nil {
		return f, nil
	}
	if errors.Is(err, errIsDirectory) {
		return s.openFile(rel + "/" + indexFile)
	}
	return nil, err
}

var errIsDirectory = errors.New("is a directory")

func (s *Site) openFile(rel string) (*File, error) {
	f, err := s.root.Open(rel)
	if err != nil {
		return nil, ErrNotFound
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, ErrNotFound
	}
	if info.IsDir() {
		f.Close()
		return nil, errIsDirectory
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, ErrNotFound
	}
	return &File{
		ReadSeekCloser: f,
		Name:           rel,
		Size:           info.Size(),
		ModTime:        info.ModTime(),
	}, nil
}

// cleanPath validates a decoded request path and returns it as a relative
// slash-separated path. It rejects rather than normalizes: any "..", ".",
// backslash, null byte, dotted or forbidden segment fails. The empty
// result ("" from "/") addresses the folder itself.
func cleanPath(p string) (string, bool) {
	if len(p) > maxPathLength {
		return "", false
	}
	if strings.ContainsAny(p, "\\\x00") {
		return "", false
	}
	// Request paths are absolute within the site; a path not starting with
	// "/" (including Windows drive forms like "C:/...") is rejected.
	if !strings.HasPrefix(p, "/") {
		return "", false
	}
	segments := strings.Split(p[1:], "/")
	kept := segments[:0]
	for i, seg := range segments {
		if seg == "" {
			// Allow a single trailing slash (directory form); reject
			// empty segments elsewhere ("//", which some servers collapse).
			if i == len(segments)-1 {
				continue
			}
			return "", false
		}
		if len(seg) > maxSegmentLength {
			return "", false
		}
		if strings.HasPrefix(seg, ".") {
			// Covers ".", "..", dotfiles and dot-directories.
			return "", false
		}
		if forbiddenName(seg) {
			return "", false
		}
		kept = append(kept, seg)
	}
	return strings.Join(kept, "/"), true
}

func forbiddenName(seg string) bool {
	for _, name := range neverServe {
		if strings.EqualFold(seg, name) {
			return true
		}
	}
	return false
}

// CleanPath applies the serving rules to a request path ("/a/b.jpg") and
// returns it relative and slash-separated ("a/b.jpg"; "" for the folder
// itself), or false when the path is unsafe or names something never
// served. Other folders served by the node use it so every folder follows
// the same rules.
func CleanPath(p string) (string, bool) { return cleanPath(p) }

// HiddenName reports whether a file or folder name is never served or
// listed: dotfiles, the agent instruction files and over-long names.
func HiddenName(name string) bool {
	return name == "" || name[0] == '.' || forbiddenName(name) || len(name) > maxSegmentLength
}
