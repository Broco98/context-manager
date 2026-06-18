package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/kimhyoyeon/context-manager/internal/knowledge"
)

func TestRunKnowAddThenSearch(t *testing.T) {
	home := t.TempDir()
	if _, err := runKnowAdd(home, knowledge.PageInput{
		Projects: []string{"front"}, Topic: "routing", Body: "uses next router",
		SourceTask: "t", When: "2026-06-18",
	}); err != nil {
		t.Fatal(err)
	}
	res, err := runKnowSearch(home, knowledge.Query{Text: "router"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].Topic != "routing" {
		t.Errorf("search failed: %+v", res)
	}
	idx, err := runKnowIndex(home)
	if err != nil || !contains(idx, "routing") {
		t.Errorf("index missing page: %v %q", err, idx)
	}
}

// TestRunKnowAddRendersSourceTask asserts the originating task is written into
// the page's sources: provenance, not left empty.
func TestRunKnowAddRendersSourceTask(t *testing.T) {
	home := t.TempDir()
	path, err := runKnowAdd(home, knowledge.PageInput{
		Projects: []string{"front"}, Topic: "routing", Body: "uses next router",
		SourceTask: "add-payment", When: "2026-06-18",
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(string(data), "task: add-payment") {
		t.Errorf("rendered page missing source task provenance:\n%s", data)
	}
}

// TestRunKnowAddRequiresSourceTask asserts consolidation refuses empty provenance.
func TestRunKnowAddRequiresSourceTask(t *testing.T) {
	home := t.TempDir()
	if _, err := runKnowAdd(home, knowledge.PageInput{
		Projects: []string{"front"}, Topic: "routing", Body: "x", When: "2026-06-18",
	}); err == nil {
		t.Error("expected USAGE error when source task is empty")
	}
}

// TestRunKnowAddRejectsInvalidCategory asserts the controlled vocabulary is
// enforced at the CLI boundary (USAGE error), and a valid category is accepted.
func TestRunKnowAddRejectsInvalidCategory(t *testing.T) {
	home := t.TempDir()
	if _, err := runKnowAdd(home, knowledge.PageInput{
		Projects: []string{"front"}, Topic: "routing", Body: "x",
		SourceTask: "t", When: "2026-06-18", Category: "bogus",
	}); err == nil {
		t.Error("expected USAGE error for an invalid --category")
	}
	if _, err := runKnowAdd(home, knowledge.PageInput{
		Projects: []string{"front"}, Topic: "routing", Body: "x",
		SourceTask: "t", When: "2026-06-18", Category: "gotcha",
	}); err != nil {
		t.Errorf("valid category should be accepted: %v", err)
	}
}

// TestKnowSearchJSONGolden locks the `know search` result contract. The page
// path is made stable by stripping the temp-home prefix before comparison.
func TestKnowSearchJSONGolden(t *testing.T) {
	home := t.TempDir()
	if _, err := runKnowAdd(home, knowledge.PageInput{
		Projects: []string{"front"}, Topic: "routing", Body: "uses next router",
		SourceTask: "add-payment", When: "2026-06-18",
	}); err != nil {
		t.Fatal(err)
	}
	res, err := runKnowSearch(home, knowledge.Query{Text: "router"})
	if err != nil {
		t.Fatal(err)
	}
	// Normalize the absolute path so the golden is reproducible across machines.
	for i := range res {
		res[i].Path = strings.TrimPrefix(res[i].Path, home)
	}
	assertGolden(t, "know_search.json", map[string]any{"results": res})
}
