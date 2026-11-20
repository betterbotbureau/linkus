package node

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/betterbotbureau/linkus/internal/identity"
)

// Export and restore: one tar.gz holding the data
// directory (identity, state, request log) and the public folder, so a node
// moves machines with its ID intact. The archive contains the private key —
// the CLI says so plainly. On restore, every archive entry is hostile input.

const (
	exportDataPrefix   = "data/"
	exportPublicPrefix = "public/"
	// restoreMaxFileBytes and restoreMaxTotalBytes bound what a hostile or
	// damaged archive can write; restoreMaxEntries bounds inode use.
	restoreMaxFileBytes  = 256 << 20
	restoreMaxTotalBytes = 1 << 30
	restoreMaxEntries    = 20000
)

// Export writes the archive to w.
func Export(w io.Writer, dataDir, publicDir string) error {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	if err := exportTree(tw, dataDir, exportDataPrefix); err != nil {
		return err
	}
	if err := exportTree(tw, publicDir, exportPublicPrefix); err != nil {
		return err
	}
	if err := tw.Close(); err != nil {
		return err
	}
	return gz.Close()
}

func exportTree(tw *tar.Writer, dir, prefix string) error {
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		name := prefix + filepath.ToSlash(rel)
		if d.IsDir() {
			return tw.WriteHeader(&tar.Header{Name: name + "/", Typeflag: tar.TypeDir, Mode: 0o755})
		}
		if !d.Type().IsRegular() {
			return nil // symlinks and specials do not travel
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if err := tw.WriteHeader(&tar.Header{
			Name:    name,
			Size:    info.Size(),
			Mode:    0o600,
			ModTime: info.ModTime(),
		}); err != nil {
			return err
		}
		f, err := os.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(tw, f)
		return err
	})
}

// Restore unpacks an archive into the two directories. It refuses to
// replace an existing identity unless force is set.
func Restore(r io.Reader, dataDir, publicDir string, force bool) error {
	if _, err := os.Stat(filepath.Join(dataDir, identity.FileName)); err == nil && !force {
		return errors.New("an identity already exists here; use --force to replace it")
	}
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("not a linknode export: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)

	entries, total := 0, int64(0)
	sawIdentity := false
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("damaged archive: %w", err)
		}
		if entries++; entries > restoreMaxEntries {
			return errors.New("archive holds too many entries")
		}
		dir, rel, ok := splitExportName(hdr.Name)
		if !ok {
			return fmt.Errorf("archive entry %q does not belong to a linknode export", hdr.Name)
		}
		base := dataDir
		perm := os.FileMode(0o600)
		if dir == exportPublicPrefix {
			base, perm = publicDir, 0o644
		}
		target := filepath.Join(base, filepath.FromSlash(rel))
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if hdr.Size > restoreMaxFileBytes {
				return fmt.Errorf("archive entry %q is unreasonably large", hdr.Name)
			}
			if total += hdr.Size; total > restoreMaxTotalBytes {
				return errors.New("archive is unreasonably large")
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, io.LimitReader(tr, hdr.Size)); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
			if dir == exportDataPrefix && rel == identity.FileName {
				sawIdentity = true
			}
		default:
			// Links and specials never restore: an archive must not be able
			// to point the node at files outside its directories.
			return fmt.Errorf("archive entry %q has a type that does not restore", hdr.Name)
		}
	}
	if !sawIdentity {
		return errors.New("archive holds no identity; nothing was a linknode export")
	}
	return nil
}

// splitExportName validates one archive path: under data/ or public/, clean,
// relative, no traversal.
func splitExportName(name string) (prefix, rel string, ok bool) {
	for _, p := range []string{exportDataPrefix, exportPublicPrefix} {
		if rest, found := strings.CutPrefix(name, p); found {
			rest = strings.TrimSuffix(rest, "/")
			if rest == "" || rest != path.Clean(rest) || path.IsAbs(rest) ||
				rest == ".." || strings.HasPrefix(rest, "../") || strings.Contains(rest, "\\") {
				return "", "", false
			}
			return p, rest, true
		}
	}
	return "", "", false
}
