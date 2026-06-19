package assets

import (
	"strings"
	"testing"
)

// The build itself fails if either embed path stops resolving (file moved or
// renamed), which is the real drift guard. These tests add a content sanity
// check so an empty or wrong asset can't ship silently.

func TestEmbeddedSkillNonEmpty(t *testing.T) {
	if !strings.Contains(SkillMD, "name: context-manager") {
		t.Errorf("SkillMD missing skill frontmatter; got %d bytes", len(SkillMD))
	}
}

func TestEmbeddedSnippetNonEmpty(t *testing.T) {
	if !strings.Contains(Snippet, "cross-repo") {
		t.Errorf("Snippet missing expected text; got %d bytes", len(Snippet))
	}
}
