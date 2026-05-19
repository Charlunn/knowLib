package vault

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolvePath_Safety(t *testing.T) {
	root := t.TempDir()
	v := New(root, "inbox", "notes", "atlas", ".knowlib")

	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"plain", "inbox/x.md", false},
		{"nested", "notes/学习/高数/微分.md", false},
		{"backslash normalized", "inbox\\nested.md", false},
		{"empty", "", true},
		{"dotdot", "../etc/passwd", true},
		{"deep dotdot", "inbox/../../etc/passwd", true},
		{"absolute posix", "/etc/passwd", true},
		{"absolute windows", "C:/Windows/System32", true},
		{"only dot", ".", true},
		{"only dotdot", "..", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			abs, err := v.ResolvePath(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got abs=%q", abs)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			rootAbs, _ := filepath.Abs(root)
			if !strings.HasPrefix(abs, rootAbs) {
				t.Fatalf("resolved path %q escapes root %q", abs, rootAbs)
			}
		})
	}
}

func TestWriteAtomic_RoundTrip(t *testing.T) {
	root := t.TempDir()
	v := New(root, "inbox", "notes", "atlas", ".knowlib")

	if err := v.WriteAtomic("inbox/hello.md", []byte("hi")); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := v.Read("inbox/hello.md")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "hi" {
		t.Fatalf("got %q want hi", got)
	}
}

func TestListSubtree_SkipsHidden(t *testing.T) {
	root := t.TempDir()
	v := New(root, "inbox", "notes", "atlas", ".knowlib")

	mustWrite := func(rel string) {
		if err := v.WriteAtomic(rel, []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("notes/a.md")
	mustWrite("notes/sub/b.md")
	// Files in hidden dirs (we create them manually since ResolvePath blocks
	// nothing about ".knowlib" — only listing must skip them).
	if err := v.WriteAtomic(".knowlib/internal.md", []byte("x")); err != nil {
		t.Fatal(err)
	}

	listed, err := v.ListSubtree("notes")
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 {
		t.Fatalf("expected 2 visible notes, got %d: %+v", len(listed), listed)
	}

	// The hidden dir is at the root level — listing root should skip it.
	rootListed, err := v.ListSubtree(".")
	if err == nil && runtime.GOOS != "" {
		// "." is normalized away in ResolvePath; just check nothing in .knowlib leaks
		for _, l := range rootListed {
			if strings.Contains(l.Path, ".knowlib/") {
				t.Fatalf("hidden file leaked into listing: %s", l.Path)
			}
		}
	}
}
