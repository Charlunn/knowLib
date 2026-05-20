// Package vault is the safety boundary between API requests (which carry
// user-supplied path strings) and the filesystem. Every read/write/list goes
// through ResolvePath which rejects absolute paths, paths with "..", and any
// path that escapes the vault root after resolution. Tested in vault_test.go.
package vault

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var (
	ErrEscape   = errors.New("path escapes vault root")
	ErrAbsolute = errors.New("path must be relative")
	ErrNotFound = errors.New("not found")
)

type Vault struct {
	Root      string
	InboxDir  string
	NotesDir  string
	AtlasDir  string
	HiddenDir string // .knowlib — never returned via API listings
}

func New(root, inbox, notes, atlas, hidden string) *Vault {
	return &Vault{
		Root:      filepath.Clean(root),
		InboxDir:  inbox,
		NotesDir:  notes,
		AtlasDir:  atlas,
		HiddenDir: hidden,
	}
}

// ResolvePath turns a vault-relative request path (like "inbox/x.md" with
// forward slashes) into a safe absolute filesystem path. It rejects:
//   - Absolute paths (Windows or POSIX)
//   - Paths containing ".." segments after cleaning
//   - Paths that don't ultimately stay inside Root
func (v *Vault) ResolvePath(rel string) (string, error) {
	if rel == "" {
		return "", fmt.Errorf("empty path")
	}
	// Normalize separators so we accept what the API/MCP clients send.
	rel = strings.ReplaceAll(rel, "\\", "/")
	if path.IsAbs(rel) || (len(rel) > 1 && rel[1] == ':') {
		return "", ErrAbsolute
	}
	// Reject any segment-level "..": cleaning ".." away first would mask escape
	// attempts like "inbox/../../etc/passwd" that resolve outside the root.
	for _, seg := range strings.Split(rel, "/") {
		if seg == ".." {
			return "", ErrEscape
		}
	}
	cleaned := path.Clean(rel)
	if cleaned == "" || cleaned == "." {
		return "", fmt.Errorf("empty path after clean")
	}
	abs := filepath.Join(v.Root, filepath.FromSlash(cleaned))
	// Defence in depth: ensure abs really is under Root after symlink-free join.
	rootAbs, err := filepath.Abs(v.Root)
	if err != nil {
		return "", err
	}
	finalAbs, err := filepath.Abs(abs)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(finalAbs+string(os.PathSeparator), rootAbs+string(os.PathSeparator)) && finalAbs != rootAbs {
		return "", ErrEscape
	}
	return abs, nil
}

// Rel converts an absolute path back to a forward-slash, vault-relative path.
func (v *Vault) Rel(abs string) (string, error) {
	r, err := filepath.Rel(v.Root, abs)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(r), nil
}

func (v *Vault) Read(rel string) ([]byte, error) {
	abs, err := v.ResolvePath(rel)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(abs)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	return b, err
}

// WriteAtomic writes to a temp file in the same dir, fsyncs, renames in place.
// Used for inbox captures so partial writes never appear to the watcher.
func (v *Vault) WriteAtomic(rel string, data []byte) error {
	abs, err := v.ResolvePath(rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(abs), ".tmp-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, abs)
}

type Listing struct {
	Path     string    `json:"path"`
	Title    string    `json:"title"`
	Size     int64     `json:"size"`
	ModTime  time.Time `json:"mod_time"`
	Category string    `json:"category,omitempty"`
}

// ListSubtree returns every .md file under the given vault-relative subdir,
// excluding hidden directories (.git, .knowlib, .obsidian, etc.). Title is
// derived from the filename stem (full title resolution lives in handlers).
func (v *Vault) ListSubtree(rel string) ([]Listing, error) {
	abs, err := v.ResolvePath(rel)
	if err != nil {
		return nil, err
	}
	var out []Listing
	err = filepath.WalkDir(abs, func(p string, d fs.DirEntry, walkErr error) error {		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) && p == abs {
				return nil // empty subtree is fine
			}
			return walkErr
		}
		if d.IsDir() {
			name := d.Name()
			if name == v.HiddenDir || strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") || strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rp, err := v.Rel(p)
		if err != nil {
			return err
		}
		out = append(out, Listing{
			Path:    rp,
			Title:   strings.TrimSuffix(d.Name(), ".md"),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if out == nil {
		out = []Listing{}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime.After(out[j].ModTime) })
	return out, nil
}

// Delete removes a file under the vault. Used to drop stale captures, etc.
func (v *Vault) Delete(rel string) error {
	abs, err := v.ResolvePath(rel)
	if err != nil {
		return err
	}
	if err := os.Remove(abs); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	return nil
}
