package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHomeUsesEnvOverride(t *testing.T) {
	t.Setenv("CTX_HOME", "/tmp/custom-ctx")
	got, err := Home()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/custom-ctx" {
		t.Errorf("got %q, want /tmp/custom-ctx", got)
	}
}

func TestHomeDefaultsToHomeDotCtx(t *testing.T) {
	t.Setenv("CTX_HOME", "")
	got, err := Home()
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	if got != filepath.Join(home, ".ctx") {
		t.Errorf("got %q, want %q", got, filepath.Join(home, ".ctx"))
	}
}

func TestAtomicWriteCreatesParentsAndContent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a", "b", "task.yaml")
	if err := AtomicWrite(p, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	if string(got) != "hello" {
		t.Errorf("got %q", string(got))
	}
}

func TestListTasksExcludesUnderscoreAndSortsByName(t *testing.T) {
	home := t.TempDir()
	for _, name := range []string{"zeta", "alpha", "_knowledge"} {
		d := filepath.Join(home, name)
		_ = os.MkdirAll(d, 0o755)
		if name != "_knowledge" {
			_ = os.WriteFile(filepath.Join(d, "task.yaml"), []byte("name: "+name), 0o644)
		}
	}
	got, err := ListTasks(home)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"alpha", "zeta"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestValidNameRejectsAdversarialInputs exercises the path-component validator
// that guards every task/project dir name. These inputs are the entire reason the
// function exists; previously none of them were asserted (0% direct coverage).
func TestValidNameRejectsAdversarialInputs(t *testing.T) {
	bad := []string{
		"",           // empty
		".",          // current dir
		"..",         // parent dir
		"../other",   // path-escape attempt
		"a/b",        // embedded slash
		"a\\b",       // embedded backslash
		"_knowledge", // shadows the reserved knowledge dir
		"_shared",    // shadows the reserved shared dir
		"_x",         // any underscore-prefixed name is reserved
		"a\x00b",     // NUL byte
	}
	for _, name := range bad {
		if err := ValidName(name); err == nil {
			t.Errorf("ValidName(%q) = nil, want error", name)
		}
	}
	for _, name := range []string{"add-payment", "front", "back.v2", "a"} {
		if err := ValidName(name); err != nil {
			t.Errorf("ValidName(%q) = %v, want nil", name, err)
		}
	}
}

// TestValidRelPathRejectsEscapesAndAbsolutes covers the containment validator's
// rejection of absolute paths and lexical "../" escapes, plus the happy path
// returning a canonical absolute form beneath base.
func TestValidRelPathRejectsEscapesAndAbsolutes(t *testing.T) {
	base := t.TempDir()
	for _, rel := range []string{"", "/etc/passwd", "../escape", "a/../../escape"} {
		if _, err := ValidRelPath(base, rel); err == nil {
			t.Errorf("ValidRelPath(base, %q) = nil, want error", rel)
		}
	}
	got, err := ValidRelPath(base, "front/wt")
	if err != nil {
		t.Fatalf("contained path errored: %v", err)
	}
	cbase, _ := filepath.EvalSymlinks(base)
	if !strings.HasPrefix(got, cbase) {
		t.Errorf("canonical path %q not under base %q", got, cbase)
	}
}

// TestValidRelPathRejectsSymlinkEscape is the security-boundary test: a symlink
// INSIDE base pointing OUTSIDE it has no ".." in the relative path, so a lexical
// check would pass — only canonicalization (resolveExisting) catches it.
func TestValidRelPathRejectsSymlinkEscape(t *testing.T) {
	base := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(base, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if _, err := ValidRelPath(base, "link/secret"); err == nil {
		t.Error("ValidRelPath followed a symlink escaping base; want error")
	}
}
