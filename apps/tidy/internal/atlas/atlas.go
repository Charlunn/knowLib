// Package atlas maintains MOC (map-of-content) files in vault/atlas/<topic>.md.
//
// The LLM proposes moc_updates as a list of {path, section, add_link}. We
// append-only by default — each (path, section, link) tuple is added at most
// once per file. Deduplication is line-level: if the link already appears
// anywhere in the section, we skip.
package atlas

import (
	"errors"
	"strings"

	"github.com/charlunn/knowlib/tidy/internal/vaultfs"
)

type Update struct {
	Path    string `json:"path"`    // e.g. "atlas/高等数学.md"
	Section string `json:"section"` // e.g. "微分方程"
	AddLink string `json:"add_link"` // e.g. "[[一阶线性微分方程]]"
}

func Apply(fs *vaultfs.FS, updates []Update) ([]string, error) {
	var applied []string
	for _, u := range updates {
		if u.Path == "" || u.AddLink == "" {
			continue
		}
		if err := applyOne(fs, u); err != nil {
			// Errors on individual MOC updates shouldn't abort the whole tidy
			// — the body has already been written. Best effort.
			continue
		}
		applied = append(applied, u.Path)
	}
	return applied, nil
}

func applyOne(fs *vaultfs.FS, u Update) error {
	body, err := fs.Read(u.Path)
	if err != nil {
		if errors.Is(err, vaultfs.ErrNotFound) {
			body = []byte(initialMOC(u.Path))
		} else {
			return err
		}
	}
	updated := insertLink(string(body), u.Section, u.AddLink)
	if updated == string(body) {
		return nil
	}
	return fs.WriteAtomic(u.Path, []byte(updated))
}

// insertLink appends the link under the given H2 section, creating the section
// at the end of the file if it doesn't exist. If the link already appears in
// the section (case-sensitive), it's left alone.
func insertLink(body, section, link string) string {
	section = strings.TrimSpace(section)
	link = strings.TrimSpace(link)

	// Section search is line-based for "## <section>" or "### <section>".
	lines := strings.Split(body, "\n")
	startIdx := -1
	endIdx := len(lines)
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if startIdx < 0 {
			if t == "## "+section || t == "### "+section {
				startIdx = i + 1
				continue
			}
			continue
		}
		// Found section start; end at next H2 or H3 boundary.
		t = strings.TrimSpace(ln)
		if strings.HasPrefix(t, "## ") || strings.HasPrefix(t, "### ") {
			endIdx = i
			break
		}
	}

	if startIdx < 0 {
		// Section missing: append a new section at end.
		buf := strings.TrimRight(body, "\n") + "\n\n## " + section + "\n\n- " + link + "\n"
		return buf
	}

	// Already present?
	for i := startIdx; i < endIdx; i++ {
		if strings.Contains(lines[i], link) {
			return body
		}
	}

	// Insert before the section's trailing blank lines.
	insertAt := endIdx
	for insertAt > startIdx && strings.TrimSpace(lines[insertAt-1]) == "" {
		insertAt--
	}
	newLine := "- " + link
	lines = append(lines[:insertAt], append([]string{newLine}, lines[insertAt:]...)...)
	return strings.Join(lines, "\n")
}

func initialMOC(path string) string {
	stem := path
	if i := strings.LastIndex(stem, "/"); i >= 0 {
		stem = stem[i+1:]
	}
	stem = strings.TrimSuffix(stem, ".md")
	return "# " + stem + "\n\n这是 AI 自动维护的主题地图。\n"
}
