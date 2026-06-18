package task

import (
	"strings"
	"testing"
)

func TestContextTemplateHasRequiredHeadings(t *testing.T) {
	out := ContextTemplate("add-payment", "needs payment")
	for _, h := range []string{"# add-payment", "## Background", "## Plan", "## Decisions", "## Journal"} {
		if !strings.Contains(out, h) {
			t.Errorf("template missing %q\n%s", h, out)
		}
	}
	if !strings.Contains(out, "needs payment") {
		t.Errorf("background not prefilled:\n%s", out)
	}
}
