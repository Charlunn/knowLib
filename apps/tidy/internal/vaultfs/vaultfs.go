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

// RemoveEmptyDirs removes empty directories under `rel` up to (but not
// including) `stopAt`. Used to clean up inbox sub-folders after all files
// inside have been tidied.
//
// Example: RemoveEmptyDirs("inbox/notion/math", "inbox") removes
// "inbox/notion/math" and "inbox/notion" if they become empty, but never
// removes "inbox" itself.
func (f *FS) RemoveEmptyDirs(rel, stopAt string) error {
	stopAbs, err := f.Resolve(stopAt)
	if err != nil {
		return err
	}
	cur := rel
	for {
		if cur == stopAt || cur == "." || cur == "" {
			break
		}
		abs, err := f.Resolve(cur)
		if err != nil {
			break
		}
		// Don't go above the stop boundary.
		if abs == stopAbs {
			break
		}
		entries, err := os.ReadDir(abs)
		if err != nil {
			break
		}
		if len(entries) > 0 {
			break // not empty, stop climbing
		}
		if err := os.Remove(abs); err != nil {
			break
		}
		cur = filepath.ToSlash(filepath.Dir(cur))
	}
	return nil
}

// ArchiveInbox moves a tidied inbox file to .knowlib/inbox-archive/<YYYY-MM>/.
// Used after successful tidy so the original is preserved (read-only history)
// instead of just deleted. Returns the archive path so the caller can log it.
//
// rel is the vault-relative source path. The archived filename is the basename
// only — the inbox sub-directory structure is flattened. If a name conflict
// exists (rare), a numeric suffix is appended.
func (f *FS) ArchiveInbox(rel string) (string, error) {
	now := time.Now()
	yearMonth := now.Format("2006-01")
	base := filepath.Base(rel)
	dest := f.KnowlibDir + "/inbox-archive/" + yearMonth + "/" + base

	srcAbs, err := f.Resolve(rel)
	if err != nil {
		return "", err
	}
	destAbs, err := f.Resolve(dest)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(destAbs), 0o755); err != nil {
		return "", err
	}

	// Handle name conflicts by appending -1, -2, ...
	finalDestAbs := destAbs
	finalDestRel := dest
	if _, err := os.Stat(finalDestAbs); err == nil {
		stem := strings.TrimSuffix(base, filepath.Ext(base))
		ext := filepath.Ext(base)
		for i := 1; i < 1000; i++ {
			altName := fmt.Sprintf("%s-%d%s", stem, i, ext)
			altRel := f.KnowlibDir + "/inbox-archive/" + yearMonth + "/" + altName
			altAbs, err := f.Resolve(altRel)
			if err != nil {
				continue
			}
			if _, err := os.Stat(altAbs); errors.Is(err, fs.ErrNotExist) {
				finalDestAbs = altAbs
				finalDestRel = altRel
				break
			}
		}
	}

	if err := os.Rename(srcAbs, finalDestAbs); err != nil {
		return "", err
	}
	return finalDestRel, nil
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

// ListInbox returns files that should be tidied: everything under InboxDir,
// plus any .md files at the vault root (since Obsidian creates new notes
// there by default when clicking unresolved wikilinks).
//
// Files under known managed subdirectories (NotesDir, AtlasDir, KnowlibDir)
// are NEVER returned, so already-tidied notes and archives are safe.
//
// Sorted by mtime ascending so older files are tidied first.
func (f *FS) ListInbox() ([]string, error) {
	type entry struct {
		Path string
		Mod  time.Time
	}
	var rows []entry

	// Excluded top-level dirs (these are managed; never reprocess).
	excluded := map[string]bool{
		f.NotesDir:   true,
		f.AtlasDir:   true,
		f.KnowlibDir: true,
		f.InboxDir:   true, // handled separately below
	}

	// 1. Vault root: only top-level .md files (not recursing).
	rootAbs, err := f.Resolve(".")
	// "." is normally rejected; resolve via empty Root walk instead.
	if err != nil {
		rootAbs = f.Root
	}
	rootEntries, err := os.ReadDir(rootAbs)
	if err == nil {
		for _, e := range rootEntries {
			name := e.Name()
			if e.IsDir() {
				continue // never recurse into root-level dirs here
			}
			if !strings.HasSuffix(name, ".md") || strings.HasPrefix(name, ".") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			rows = append(rows, entry{Path: name, Mod: info.ModTime()})
		}
	}

	// 2. Inbox subtree (recursive).
	inboxAbs, err := f.Resolve(f.InboxDir)
	if err == nil {
		_ = filepath.WalkDir(inboxAbs, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				if errors.Is(walkErr, fs.ErrNotExist) {
					return nil
				}
				return walkErr
			}
			if d.IsDir() {
				if strings.HasPrefix(d.Name(), ".") && p != inboxAbs {
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
	}

	_ = excluded // reserved for future safety checks

	sort.Slice(rows, func(i, j int) bool { return rows[i].Mod.Before(rows[j].Mod) })
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Path)
	}
	return out, nil
}
