package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSkillStatusEmptyHome(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".claude"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := runSkillStatus(home, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Target != "claude" || got[0].SkillInstalled {
		t.Errorf("want one uninstalled claude target, got %+v", got)
	}
}

func TestResolveTargetsNoHomeErrors(t *testing.T) {
	if _, err := resolveTargets(t.TempDir(), nil); err == nil {
		t.Error("want error when no agent home exists")
	}
}

func TestResolveTargetsExplicitMissingErrors(t *testing.T) {
	if _, err := resolveTargets(t.TempDir(), []string{"claude"}); err == nil {
		t.Error("want error when --target claude home is absent")
	}
}

func TestInstallSkillCreatesAndIsIdempotent(t *testing.T) {
	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	ct, _ := lookupTarget("claude")
	a1, p, err := installSkill(home, ct, false)
	if err != nil || a1 != "created" {
		t.Fatalf("first install: action=%q err=%v", a1, err)
	}
	b, _ := os.ReadFile(p)
	if string(b) != assetsSkillMDForTest(t) {
		t.Error("skill file content != embedded")
	}
	a2, _, _ := installSkill(home, ct, false)
	if a2 != "unchanged" {
		t.Errorf("second install action: got %q want unchanged", a2)
	}
}

func TestInstallSkillRefusesSymlinkWithoutForce(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude", "skills")
	_ = os.MkdirAll(dir, 0o755)
	target := t.TempDir() // the symlink's target; --force must replace the link, not touch this
	if err := os.Symlink(target, filepath.Join(dir, "context-manager")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	ct, _ := lookupTarget("claude")
	if _, _, err := installSkill(home, ct, false); err == nil {
		t.Error("want refusal on symlinked skill dir without --force")
	}
	// --force replaces the symlink with a real file holding the embedded skill.
	a, p, err := installSkill(home, ct, true)
	if err != nil {
		t.Fatalf("--force should replace the symlink: %v", err)
	}
	if a != "created" {
		t.Errorf("forced install action: got %q want created", a)
	}
	// (a) the skill dir is now a real directory, not the symlink.
	if fi, lerr := os.Lstat(filepath.Dir(p)); lerr != nil {
		t.Fatalf("lstat skill dir: %v", lerr)
	} else if fi.Mode()&os.ModeSymlink != 0 {
		t.Error("skill dir is still a symlink after --force")
	}
	// (b) the written content is the embedded skill.
	if b, _ := os.ReadFile(p); string(b) != assetsSkillMDForTest(t) {
		t.Error("forced install content != embedded SKILL.md")
	}
	// (c) os.Remove drops the link, never its target — the target dir stays empty.
	if entries, derr := os.ReadDir(target); derr != nil || len(entries) != 0 {
		t.Errorf("symlink target dir was modified: entries=%d err=%v", len(entries), derr)
	}
}

func TestUpsertSnippetIdempotentAndPreservesContent(t *testing.T) {
	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	ct, _ := lookupTarget("claude")
	p := filepath.Join(home, ".claude", "CLAUDE.md")
	_ = os.WriteFile(p, []byte("# my prefs\nkeep me\n"), 0o644)

	a1, _, err := upsertSnippet(home, ct)
	if err != nil || a1 != "appended" {
		t.Fatalf("first upsert: action=%q err=%v", a1, err)
	}
	a2, _, _ := upsertSnippet(home, ct)
	if a2 != "unchanged" {
		t.Errorf("second upsert action: got %q want unchanged", a2)
	}
	body, _ := os.ReadFile(p)
	if !strings.Contains(string(body), "keep me") {
		t.Error("unrelated user content was lost")
	}
	if strings.Count(string(body), snippetBegin) != 1 {
		t.Errorf("want exactly one snippet block, got %d", strings.Count(string(body), snippetBegin))
	}
}

func TestUpsertSnippetReplacesEditedBlock(t *testing.T) {
	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	ct, _ := lookupTarget("claude")
	p := filepath.Join(home, ".claude", "CLAUDE.md")
	_ = os.WriteFile(p, []byte(snippetBegin+"\nstale\n"+snippetEnd+"\n"), 0o644)
	a, _, _ := upsertSnippet(home, ct)
	if a != "updated" {
		t.Errorf("action: got %q want updated", a)
	}
	b, _ := os.ReadFile(p)
	if strings.Contains(string(b), "stale") {
		t.Error("stale block content was not replaced")
	}
	// The rewritten block must be byte-identical to the canonical idempotent form:
	// re-running is "unchanged", exactly one block, no doubled trailing newline.
	a2, _, _ := upsertSnippet(home, ct)
	if a2 != "unchanged" {
		t.Errorf("re-run after update: got %q want unchanged (newline math drifted)", a2)
	}
	b2, _ := os.ReadFile(p)
	if n := strings.Count(string(b2), snippetBegin); n != 1 {
		t.Errorf("want exactly one block after update, got %d", n)
	}
	if strings.Contains(string(b2), snippetEnd+"\n\n") {
		t.Error("accumulated blank line after update — trailing newline doubled")
	}
}

func TestRunSkillInstallActionsAndPaths(t *testing.T) {
	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	got, err := runSkillInstall(home, nil, false)
	if err != nil || len(got) != 1 {
		t.Fatalf("install: got %+v err=%v", got, err)
	}
	r := got[0]
	if r.Target != "claude" || r.SkillAction != "created" || r.SnippetAction != "created" {
		t.Errorf("unexpected actions: %+v", r)
	}
	if !strings.HasSuffix(r.SkillPath, filepath.Join(".claude", "skills", "context-manager", "SKILL.md")) {
		t.Errorf("skillPath suffix: %s", r.SkillPath)
	}
}

func TestSkillStatusGolden(t *testing.T) {
	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	if _, err := runSkillInstall(home, nil, false); err != nil {
		t.Fatal(err)
	}
	got, err := runSkillStatus(home, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertGolden(t, "skill_status.json", got)
}

// TestUpsertSnippetRefusesMalformedBlock guards the data-loss path found in review:
// a begin marker without an end (or an inverted pair) must NOT append a second block
// — it refuses and leaves the user's content byte-for-byte untouched.
func TestUpsertSnippetRefusesMalformedBlock(t *testing.T) {
	ct, _ := lookupTarget("claude")
	for _, seed := range []string{
		"# notes\n" + snippetBegin + "\nMY HAND-EDITED NOTES\n",        // begin without end
		"# notes\n" + snippetEnd + "\nstray end then\n" + snippetBegin, // end before begin
	} {
		home := t.TempDir()
		_ = os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
		p := filepath.Join(home, ".claude", "CLAUDE.md")
		_ = os.WriteFile(p, []byte(seed), 0o644)
		if _, _, err := upsertSnippet(home, ct); err == nil {
			t.Errorf("malformed block %q: want error, got nil", seed)
		}
		if b, _ := os.ReadFile(p); string(b) != seed {
			t.Errorf("malformed block was modified; want untouched.\n got: %q\nwant: %q", string(b), seed)
		}
	}
}

func TestUpsertSnippetAppendsBlankLineWhenNoTrailingNewline(t *testing.T) {
	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	ct, _ := lookupTarget("claude")
	p := filepath.Join(home, ".claude", "CLAUDE.md")
	_ = os.WriteFile(p, []byte("# my prefs\nno newline"), 0o644) // no trailing newline
	a, _, err := upsertSnippet(home, ct)
	if err != nil || a != "appended" {
		t.Fatalf("append: action=%q err=%v", a, err)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "no newline\n\n"+snippetBegin) {
		t.Errorf("want a blank line between prior content and the block; got:\n%s", string(b))
	}
	if a2, _, _ := upsertSnippet(home, ct); a2 != "unchanged" {
		t.Errorf("re-run: got %q want unchanged", a2)
	}
}

func TestInstallSkillUpdatesStaleFile(t *testing.T) {
	home := t.TempDir()
	ct, _ := lookupTarget("claude")
	dir := filepath.Join(home, ".claude", skillSubdir)
	_ = os.MkdirAll(dir, 0o755)
	p := filepath.Join(dir, skillFile)
	_ = os.WriteFile(p, []byte("STALE — pre-upgrade content\n"), 0o644)
	a, _, err := installSkill(home, ct, false)
	if err != nil || a != "updated" {
		t.Fatalf("install: action=%q err=%v want updated", a, err)
	}
	if b, _ := os.ReadFile(p); string(b) != assetsSkillMDForTest(t) {
		t.Error("stale skill file was not rewritten to the embedded SkillMD")
	}
}

func TestRunSkillStatusDetectsDrift(t *testing.T) {
	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	if _, err := runSkillInstall(home, nil, false); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(home, ".claude", skillSubdir, skillFile)
	b, _ := os.ReadFile(p)
	_ = os.WriteFile(p, append(b, []byte("\n# drifted\n")...), 0o644)
	got, err := runSkillStatus(home, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !got[0].SkillInstalled || got[0].SkillMatchesEmbedded {
		t.Errorf("want installed-but-drifted, got %+v", got)
	}
}

func TestRunSkillInstallCodexTarget(t *testing.T) {
	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	got, err := runSkillInstall(home, []string{"codex"}, false)
	if err != nil || len(got) != 1 {
		t.Fatalf("install: got %+v err=%v", got, err)
	}
	r := got[0]
	if r.Target != "codex" {
		t.Errorf("target: got %q want codex", r.Target)
	}
	if !strings.HasSuffix(r.SkillPath, filepath.Join(".codex", "skills", "context-manager", "SKILL.md")) {
		t.Errorf("skillPath suffix: %s", r.SkillPath)
	}
	b, err := os.ReadFile(filepath.Join(home, ".codex", "AGENTS.md"))
	if err != nil {
		t.Fatalf("AGENTS.md not written: %v", err)
	}
	if !strings.Contains(string(b), snippetBegin) {
		t.Error("snippet block not written into AGENTS.md")
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "CLAUDE.md")); err == nil {
		t.Error("snippet wrongly written to CLAUDE.md for codex target")
	}
}

func TestRunSkillInstallBothTargetsAutoDetect(t *testing.T) {
	home := t.TempDir()
	_ = os.MkdirAll(filepath.Join(home, ".claude"), 0o755)
	_ = os.MkdirAll(filepath.Join(home, ".codex"), 0o755)
	got, err := runSkillInstall(home, nil, false)
	if err != nil || len(got) != 2 {
		t.Fatalf("install: got %+v err=%v", got, err)
	}
	if got[0].Target != "claude" || got[1].Target != "codex" {
		t.Errorf("target ordering: %+v", got)
	}
	if !strings.HasSuffix(got[0].SnippetFile, "CLAUDE.md") || !strings.HasSuffix(got[1].SnippetFile, "AGENTS.md") {
		t.Errorf("snippet files: %+v", got)
	}
	if st, err := runSkillStatus(home, nil); err != nil || len(st) != 2 {
		t.Fatalf("status: got %+v err=%v", st, err)
	}
}

func TestResolveTargetsUnknownNameErrors(t *testing.T) {
	if _, err := resolveTargets(t.TempDir(), []string{"emacs"}); err == nil {
		t.Fatal("want error for unrecognized --target name")
	}
}

// assetsSkillMDForTest reads the canonical SKILL.md from disk (relative to this
// test package) to confirm the installed file equals the embedded source.
func assetsSkillMDForTest(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "skill", "context-manager", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
