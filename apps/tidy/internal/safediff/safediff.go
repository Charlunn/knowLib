// Package safediff implements the hard guarantee underlying the user's
// "AI must not change my words" promise.
//
// The LLM returns body_with_links, which is supposed to be the original body
// with [[wikilinks]] inserted at semantically appropriate spots. We trust
// nothing about that claim — instead we strip every wikilink wrapper from the
// LLM's output and check the result is byte-equivalent to the original body
// (with light whitespace tolerance). If even one character of the user's text
// has been altered, we reject and fall back to the minimal-frontmatter outcome.
//
// What's allowed:
//   - inserting [[Term]] around an existing word "Term"
//   - inserting [[Term|alias]] around an existing word "alias" (aliased link)
//   - inserting [[Term#Heading]] around an existing word "Term"
//   - inserting [[Term#Heading|alias]] around an existing word "alias"
//
// What's NOT allowed:
//   - changing any character outside wikilink wrappers
//   - inserting wikilinks inside fenced code blocks (we don't try to be clever
//     here — just reject any insertion whose [[…]] target falls inside a fence)
//   - rewording, reformatting, deleting, or reordering text
package safediff

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ErrModified is returned when the LLM's body_with_links is not a
// wikilink-only superset of the original. The caller should fall back to a
// frontmatter-only edit and leave the body untouched.
var ErrModified = errors.New("body_with_links modified non-wikilink content")

// wikilinkRe matches [[target]], [[target|alias]], [[target#section]],
// [[target#section|alias]]. We capture the visible text (alias if present,
// else target's display form) so we can substitute it back when comparing
// against the original.
//
// Note: this is a regex, not a real parser. It does NOT understand nested
// [[...]] (which are not valid wikilinks anyway) and intentionally rejects
// brackets containing newlines so a stray [[ at end of line can't trigger
// a multi-line match.
var wikilinkRe = regexp.MustCompile(`\[\[([^\[\]\n|#]+)(?:#[^\[\]\n|]+)?(?:\|([^\[\]\n]+))?\]\]`)

// fenceRe matches the start/end of a fenced code block at line start (``` or ~~~,
// possibly with a language tag).
var fenceRe = regexp.MustCompile(`(?m)^(\x60{3,}|~{3,})[^\n]*$`)

// CheckBodyWithLinks compares the LLM's output against the original body.
// On success, returns the (cleaned, normalized) body that should be persisted —
// in practice that's `withLinks` itself, but normalised for trailing whitespace.
//
// The check is whitespace-insensitive only for trailing whitespace and a single
// optional trailing newline. Internal whitespace must match exactly so the LLM
// can't silently re-flow paragraphs.
func CheckBodyWithLinks(original, withLinks string) (string, error) {
	original = strings.TrimRight(original, "\r\n\t ")
	withLinks = strings.TrimRight(withLinks, "\r\n\t ")

	// Reject any wikilink whose insertion location lies inside a fenced code
	// block. Easier: walk the LLM output, locate fence ranges, and check no
	// wikilink match falls inside one. Our chunker preserves fences atomically,
	// so the LLM should know better, but we don't trust it.
	fenceRanges := findFenceRanges(withLinks)
	matches := wikilinkRe.FindAllStringSubmatchIndex(withLinks, -1)
	for _, m := range matches {
		start := m[0]
		if isInsideFence(start, fenceRanges) {
			return "", fmt.Errorf("%w: wikilink inserted inside code fence", ErrModified)
		}
	}

	// Strip wikilinks: replace [[X]] with "X", [[X|alias]] with "alias",
	// [[X#sec]] with "X", [[X#sec|alias]] with "alias".
	stripped := wikilinkRe.ReplaceAllStringFunc(withLinks, func(s string) string {
		m := wikilinkRe.FindStringSubmatch(s)
		// m[0]=full, m[1]=target, m[2]=alias-or-empty
		if len(m) >= 3 && m[2] != "" {
			return m[2]
		}
		// No alias: visible text is the target. Strip any "#section" suffix
		// that the regex already excluded, but be safe.
		t := m[1]
		if i := strings.IndexByte(t, '#'); i >= 0 {
			t = t[:i]
		}
		return t
	})

	if stripped != original {
		// Provide a hint about where they diverge — first differing rune index.
		idx := firstDiffIndex(original, stripped)
		return "", fmt.Errorf("%w: divergence at byte offset %d", ErrModified, idx)
	}
	return withLinks, nil
}

func firstDiffIndex(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// findFenceRanges returns [start, end) byte spans of fenced code blocks.
// Unmatched opening fences extend to end-of-string — better to over-protect
// than to allow a wikilink inside a half-open code section.
func findFenceRanges(s string) [][2]int {
	idxs := fenceRe.FindAllStringIndex(s, -1)
	var ranges [][2]int
	for i := 0; i+1 < len(idxs); i += 2 {
		open := idxs[i]
		close := idxs[i+1]
		ranges = append(ranges, [2]int{open[0], close[1]})
	}
	if len(idxs)%2 == 1 {
		open := idxs[len(idxs)-1]
		ranges = append(ranges, [2]int{open[0], len(s)})
	}
	return ranges
}

func isInsideFence(pos int, ranges [][2]int) bool {
	for _, r := range ranges {
		if pos >= r[0] && pos < r[1] {
			return true
		}
	}
	return false
}
