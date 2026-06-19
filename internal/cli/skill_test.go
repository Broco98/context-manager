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
	if err := os.Symlink(t.TempDir(), filepath.Join(dir, "context-manager")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	ct, _ := lookupTarget("claude")
	if _, _, err := installSkill(home, ct, false); err == nil {
		t.Error("want refusal on symlinked skill dir without --force")
	}
	if _, _, err := installSkill(home, ct, true); err != nil {
		t.Errorf("--force should replace the symlink: %v", err)
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
