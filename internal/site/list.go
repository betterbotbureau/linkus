package site

import (
	"io/fs"
	"sort"
)

// Entry is one file a visitor can fetch.
type Entry struct {
	Path string // request path, starting with "/"
	Size int64
}

// Skipped is something in the folder that is not served, and why.
type Skipped struct {
	Path   string
	Reason string // "dotfile", "agent instruction file", "name too long", "not a regular file"
}

// List walks the folder with the serving rules and returns what a visitor
// can fetch and what is present but hidden. It answers the owner's question
// "what exactly is public?".
func (s *Site) List() (served []Entry, skipped []Skipped, err error) {
	err = fs.WalkDir(s.root.FS(), ".", func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || path == "." {
			return walkErr
		}
		name := d.Name()
		reason := ""
		switch {
		case name[0] == '.':
			reason = "dotfile"
		case forbiddenName(name):
			reason = "agent instruction file"
		case len(name) > maxSegmentLength:
			reason = "name too long"
		}
		if reason != "" {
			skipped = append(skipped, Skipped{Path: "/" + path, Reason: reason})
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !d.Type().IsRegular() {
			skipped = append(skipped, Skipped{Path: "/" + path, Reason: "not a regular file"})
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil // vanished mid-walk; not worth failing the listing
		}
		served = append(served, Entry{Path: "/" + path, Size: info.Size()})
		return nil
	})
	sort.Slice(served, func(i, j int) bool { return served[i].Path < served[j].Path })
	sort.Slice(skipped, func(i, j int) bool { return skipped[i].Path < skipped[j].Path })
	return served, skipped, err
}
