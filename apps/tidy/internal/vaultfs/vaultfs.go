// Package vaultfs is a thin filesystem wrapper for the tidy worker. It mirrors
// the safety contract of api/internal/vault but with the operations tidy needs:
//   - List inbox files
//   - Read a file's bytes
//   - Atomically write a new file under notes/ or atlas/
//   - Remove the inbox file once tidied
//   - Append to the .knowlib log
package vaultfs

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

type FS struct {
	Root       string
	InboxDir   string
	NotesDir   string
	AtlasDir   string
	KnowlibDir string
}

func New(root, inbox, notes, atlas, hidden string) *FS {
	return &FS{
		Root:       filepath.Clean(root),
		InboxDir:   inbox,
		NotesDir:   notes,
		AtlasDir:   atlas,
		KnowlibDir: hidden,
	}
}

// Resolve returns a safe absolute path under Root for a vault-relative `rel`.
// The semantics match api/internal/vault — we duplicate here rather than share
// because the api and tidy go modules are independent.
func (f *FS) Resolve(rel string) (string, error) {
	if rel == "" {
		return "", fmt.Errorf("empty path")
	}
	rel = strings.ReplaceAll(rel, "\\", "/")
	if path.IsAbs(rel) || (len(rel) > 1 && rel[1] == ':') {
		return "", ErrAbsolute
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == ".." {
			return "", ErrEscape
		}
	}
	cleaned := path.Clean(rel)
	if cleaned == "" || cleaned == "." {
		return "", fmt.Errorf("empty path after clean")
	}
	abs := filepath.Join(f.Root, filepath.FromSlash(cleaned))
	rootAbs, err := filepath.Abs(f.Root)
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

func (f *FS) Read(rel string) ([]byte, error) {
	abs, err := f.Resolve(rel)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(abs)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	return b, err
}

// WriteAtomic writes via temp + fsync + rename so the watcher never sees a
// partial file. Creates parent dirs as needed.
func (f *FS) WriteAtomic(rel string, data []byte) error {
	abs, err := f.Resolve(rel)
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

// Exists reports whether a vault-relative path resolves to an existing file.
func (f *FS) Exists(rel string) bool {
	abs, err := f.Resolve(rel)
	if err != nil {
		return false
	}
	_, err = os.Stat(abs)
	return err == nil
}

// Remove deletes a vault-relative file. No-op if missing.
func (f *FS) Remove(rel string) error {
	abs, err := f.Resolve(rel)
	if err != nil {
		return err
	}
	if err := os.Remove(abs); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// AppendLog appends a single line (with newline) to .knowlib/<name>.
func (f *FS) AppendLog(name, line string) error {
	rel := f.KnowlibDir + "/" + name
	abs, err := f.Resolve(rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	fh, err := os.OpenFile(abs, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer fh.Close()
	_, err = fh.WriteString(time.Now().UTC().Format(time.RFC3339) + " " + line + "\n")
	return err
}

// ListInbox returns inbox files sorted by mtime ascending (oldest first).
func (f *FS) ListInbox() ([]string, error) {
	abs, err := f.Resolve(f.InboxDir)
	if err != nil {
		return nil, err
	}
	type entry struct {
		Path string
		Mod  time.Time
	}
	var rows []entry
	err = filepath.WalkDir(abs, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, fs.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && p != abs {
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
		rel, err := filepath.Rel(f.Root, p)
		if err != nil {
			return err
		}
		rows = append(rows, entry{Path: filepath.ToSlash(rel), Mod: info.ModTime()})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Mod.Before(rows[j].Mod) })
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Path)
	}
	return out, nil
}
