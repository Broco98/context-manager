package task

import "testing"

func TestContextTemplateHasRequiredHeadings(t *testing.T) {
	out := ContextTemplate("add-payment", "needs payment")
	for _, h := range []string{"# add-payment", "## Background", "## Plan", "## Decisions", "## Journal"} {
		if !contains(out, h) {
			t.Errorf("template missing %q\n%s", h, out)
		}
	}
	if !contains(out, "needs payment") {
		t.Errorf("background not prefilled:\n%s", out)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (indexOf(s, sub) >= 0)
}
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
