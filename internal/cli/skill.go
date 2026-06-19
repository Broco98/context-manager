package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	assets "github.com/kimhyoyeon/context-manager"
	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/kimhyoyeon/context-manager/internal/store"
	"github.com/spf13/cobra"
)

const (
	skillSubdir  = "skills/context-manager"
	skillFile    = "SKILL.md"
	snippetBegin = "<!-- ctx:begin -->"
	snippetEnd   = "<!-- ctx:end -->"
)

// skillTarget describes one coding agent's on-disk integration layout. Adding an
// agent is a one-row change.
type skillTarget struct {
	name, homeRel, snippetFile string
}

var skillTargets = []skillTarget{
	{name: "claude", homeRel: ".claude", snippetFile: "CLAUDE.md"},
	{name: "codex", homeRel: ".codex", snippetFile: "AGENTS.md"},
}

func lookupTarget(name string) (skillTarget, bool) {
	for _, t := range skillTargets {
		if t.name == name {
			return t, true
		}
	}
	return skillTarget{}, false
}

// resolveTargets returns the targets to act on: the named ones (erroring if a
// named target's home is absent), or every target whose home exists when none are
// named. No resolvable target is a STATE error rather than a silent no-op.
func resolveTargets(home string, names []string) ([]skillTarget, error) {
	if len(names) > 0 {
		out := make([]skillTarget, 0, len(names))
		for _, n := range names {
			t, ok := lookupTarget(n)
			if !ok {
				return nil, output.Errorf(jsonOut, output.ErrUsage, "unknown target %q (want claude or codex)", n)
			}
			dir := filepath.Join(home, t.homeRel)
			if _, err := os.Stat(dir); err != nil {
				return nil, output.Errorf(jsonOut, output.ErrState, "%s home not found at %s", t.name, dir)
			}
			out = append(out, t)
		}
		return out, nil
	}
	out := make([]skillTarget, 0, len(skillTargets))
	for _, t := range skillTargets {
		if _, err := os.Stat(filepath.Join(home, t.homeRel)); err == nil {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil, output.Errorf(jsonOut, output.ErrState, "no agent home found under %s; pass --target claude|codex", home)
	}
	return out, nil
}

// installSkill writes the embedded skill file for one target. A symlinked skill
// dir is a user's deliberate live-link (`ln -snf`) and is never clobbered without
// --force; under --force only the link is removed (os.Remove), never its target.
func installSkill(home string, t skillTarget, force bool) (action, path string, err error) {
	dir := filepath.Join(home, t.homeRel, skillSubdir)
	path = filepath.Join(dir, skillFile)
	if fi, lerr := os.Lstat(dir); lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
		if !force {
			return "", path, output.Errorf(jsonOut, output.ErrConflict,
				"%s is a symlink (manually installed); pass --force to replace", dir)
		}
		if rmErr := os.Remove(dir); rmErr != nil {
			return "", path, rmErr
		}
	}
	switch existing, rerr := os.ReadFile(path); {
	case rerr == nil && string(existing) == assets.SkillMD:
		action = "unchanged"
	case rerr == nil:
		action = "updated"
	case os.IsNotExist(rerr):
		action = "created"
	default:
		return "", path, rerr
	}
	if err := store.AtomicWrite(path, []byte(assets.SkillMD)); err != nil {
		return "", path, err
	}
	return action, path, nil
}

// upsertSnippet writes the discovery snippet into the target's global memory file
// as a marker-delimited block: replace an existing block, append after existing
// content, or create the file. Idempotent — an unchanged block is left untouched,
// and content outside the markers is preserved verbatim.
func upsertSnippet(home string, t skillTarget) (action, path string, err error) {
	path = filepath.Join(home, t.homeRel, t.snippetFile)
	block := snippetBegin + "\n" + strings.TrimSpace(assets.Snippet) + "\n" + snippetEnd + "\n"

	existing, rerr := os.ReadFile(path)
	if rerr != nil && !os.IsNotExist(rerr) {
		return "", path, rerr
	}
	content := string(existing) // "" when the file is missing

	bi := strings.Index(content, snippetBegin)
	ei := strings.Index(content, snippetEnd)
	wellFormed := bi >= 0 && ei > bi
	// A partial or inverted marker pair (begin without end, end without begin, or
	// end before begin) means a human truncated or garbled the managed block.
	// Appending a fresh block here would duplicate markers, and a later run could
	// delete the content trapped between the orphaned markers — so refuse rather
	// than corrupt, and tell the user to fix it (fail closed).
	if (bi >= 0 || ei >= 0) && !wellFormed {
		return "", path, output.Errorf(jsonOut, output.ErrState,
			"%s has a malformed ctx marker block (mismatched %s / %s); fix or remove it, then re-run", path, snippetBegin, snippetEnd)
	}
	var updated string
	switch {
	case wellFormed:
		end := ei + len(snippetEnd)
		if end < len(content) && content[end] == '\n' {
			end++ // consume one trailing newline so the block's own newline doesn't double
		}
		if content[bi:end] == block {
			return "unchanged", path, nil
		}
		action, updated = "updated", content[:bi]+block+content[end:]
	case content == "":
		action, updated = "created", block
	default:
		sep := "\n"
		if !strings.HasSuffix(content, "\n") {
			sep = "\n\n"
		}
		action, updated = "appended", content+sep+block
	}
	if err := store.AtomicWrite(path, []byte(updated)); err != nil {
		return "", path, err
	}
	return action, path, nil
}

type skillStatusOut struct {
	Target               string `json:"target"`
	SkillInstalled       bool   `json:"skillInstalled"`
	SkillMatchesEmbedded bool   `json:"skillMatchesEmbedded"`
	SnippetInstalled     bool   `json:"snippetInstalled"`
}

func runSkillStatus(home string, names []string) ([]skillStatusOut, error) {
	ts, err := resolveTargets(home, names)
	if err != nil {
		return nil, err
	}
	out := make([]skillStatusOut, 0, len(ts))
	for _, t := range ts {
		s := skillStatusOut{Target: t.name}
		if b, err := os.ReadFile(filepath.Join(home, t.homeRel, skillSubdir, skillFile)); err == nil {
			s.SkillInstalled = true
			s.SkillMatchesEmbedded = string(b) == assets.SkillMD
		}
		if b, err := os.ReadFile(filepath.Join(home, t.homeRel, t.snippetFile)); err == nil {
			// Require a well-formed begin...end pair, matching what upsertSnippet
			// treats as an installed block — a lone or inverted marker is not "installed".
			c := string(b)
			bi := strings.Index(c, snippetBegin)
			ei := strings.Index(c, snippetEnd)
			s.SnippetInstalled = bi >= 0 && ei > bi
		}
		out = append(out, s)
	}
	return out, nil
}

type skillInstallOut struct {
	Target        string `json:"target"`
	SkillPath     string `json:"skillPath"`
	SkillAction   string `json:"skillAction"`
	SnippetFile   string `json:"snippetFile"`
	SnippetAction string `json:"snippetAction"`
}

func runSkillInstall(home string, names []string, force bool) ([]skillInstallOut, error) {
	ts, err := resolveTargets(home, names)
	if err != nil {
		return nil, err
	}
	out := make([]skillInstallOut, 0, len(ts))
	for _, t := range ts {
		sa, sp, err := installSkill(home, t, force)
		if err != nil {
			return nil, err
		}
		na, np, err := upsertSnippet(home, t)
		if err != nil {
			return nil, err
		}
		out = append(out, skillInstallOut{t.name, sp, sa, np, na})
	}
	return out, nil
}

func init() {
	skillCmd := &cobra.Command{Use: "skill", Short: "Install the ctx companion skill + discovery snippet into agent homes"}

	var instTargets []string
	var force bool
	installCmd := &cobra.Command{
		Use:   "install",
		Short: "Install the skill + discovery snippet for detected agents",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			res, err := runSkillInstall(home, instTargets, force)
			if err != nil {
				return err
			}
			human := ""
			for i, r := range res {
				if i > 0 {
					human += "\n"
				}
				human += fmt.Sprintf("%s: skill %s → %s; snippet %s → %s", r.Target, r.SkillAction, r.SkillPath, r.SnippetAction, r.SnippetFile)
			}
			return output.Emit(jsonOut, human, map[string]any{"installed": res})
		},
	}
	installCmd.Flags().StringSliceVar(&instTargets, "target", nil, "claude|codex (default: auto-detect)")
	installCmd.Flags().BoolVar(&force, "force", false, "replace a manually-symlinked skill dir")

	var statTargets []string
	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show skill + snippet install status per agent",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			res, err := runSkillStatus(home, statTargets)
			if err != nil {
				return err
			}
			human := ""
			for i, r := range res {
				if i > 0 {
					human += "\n"
				}
				human += fmt.Sprintf("%s: skill=%v (matches=%v) snippet=%v", r.Target, r.SkillInstalled, r.SkillMatchesEmbedded, r.SnippetInstalled)
			}
			return output.Emit(jsonOut, human, map[string]any{"targets": res})
		},
	}
	statusCmd.Flags().StringSliceVar(&statTargets, "target", nil, "claude|codex (default: auto-detect)")

	skillCmd.AddCommand(installCmd, statusCmd)
	RootCmd.AddCommand(skillCmd)
}
