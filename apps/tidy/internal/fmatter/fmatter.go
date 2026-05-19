// Package fmatter splits + writes YAML-ish frontmatter without pulling in a
// full YAML lib. We only emit a flat top-level map (strings, ints, lists of
// strings) — that's all the tidy worker needs.
package fmatter

import (
	"fmt"
	"strings"
)

// Parse returns (frontmatterMap, body). If the input has no frontmatter,
// returns (empty map, full input).
func Parse(s string) (map[string]string, string) {
	out := map[string]string{}
	if !strings.HasPrefix(s, "---\n") && !strings.HasPrefix(s, "---\r\n") {
		return out, s
	}
	rest := strings.TrimPrefix(s, "---\n")
	rest = strings.TrimPrefix(rest, "---\r\n")
	// Find next "---" line.
	end := -1
	lines := strings.SplitAfter(rest, "\n")
	soFar := 0
	for _, ln := range lines {
		t := strings.TrimRight(ln, "\r\n")
		soFar += len(ln)
		if t == "---" {
			end = soFar - len(ln)
			break
		}
	}
	if end < 0 {
		return out, s
	}
	header := rest[:end]
	body := rest[end:]
	body = strings.TrimPrefix(body, "---\n")
	body = strings.TrimPrefix(body, "---\r\n")
	for _, line := range strings.Split(header, "\n") {
		line = strings.TrimRight(line, "\r")
		i := strings.Index(line, ":")
		if i <= 0 {
			continue
		}
		k := strings.TrimSpace(line[:i])
		v := strings.TrimSpace(line[i+1:])
		out[k] = v
	}
	return out, body
}

// Render emits a flat YAML-ish frontmatter block + body.
//
// Field type rules:
//   - string    → key: value (quoted only if contains ":" or starts with "[")
//   - []string  → key: [a, b, c]   (each element double-quoted if needed)
//   - int       → key: 42
//   - bool      → key: true|false
//
// Order of `keys` is preserved so generated frontmatter stays stable.
func Render(keys []string, fm map[string]any, body string) string {
	var b strings.Builder
	b.WriteString("---\n")
	for _, k := range keys {
		v, ok := fm[k]
		if !ok || v == nil {
			continue
		}
		switch t := v.(type) {
		case string:
			if t == "" {
				continue
			}
			fmt.Fprintf(&b, "%s: %s\n", k, scalar(t))
		case []string:
			if len(t) == 0 {
				continue
			}
			fmt.Fprintf(&b, "%s: [%s]\n", k, joinList(t))
		case int:
			fmt.Fprintf(&b, "%s: %d\n", k, t)
		case bool:
			fmt.Fprintf(&b, "%s: %t\n", k, t)
		default:
			// fallback to %v string
			fmt.Fprintf(&b, "%s: %s\n", k, scalar(fmt.Sprint(t)))
		}
	}
	b.WriteString("---\n\n")
	body = strings.TrimLeft(body, "\n")
	b.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		b.WriteString("\n")
	}
	return b.String()
}

func scalar(s string) string {
	if needsQuote(s) {
		return fmt.Sprintf("%q", s)
	}
	return s
}

func needsQuote(s string) bool {
	if s == "" {
		return true
	}
	if strings.ContainsAny(s, ":#&*!|>'\"%@`") {
		return true
	}
	if strings.HasPrefix(s, "[") || strings.HasPrefix(s, "{") || strings.HasPrefix(s, "-") {
		return true
	}
	return false
}

func joinList(ts []string) string {
	parts := make([]string, len(ts))
	for i, t := range ts {
		parts[i] = scalar(t)
	}
	return strings.Join(parts, ", ")
}
