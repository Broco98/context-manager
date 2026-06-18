package cli

import "testing"

func TestRunGoalSetUpdatesDoneWhen(t *testing.T) {
	home := t.TempDir()
	seedTask(t, home, "tk")
	g, err := runGoalSet(home, "tk", "", "all tests pass")
	if err != nil {
		t.Fatal(err)
	}
	if g.DoneWhen != "all tests pass" || g.Objective != "o" {
		t.Errorf("bad goal: %+v", g)
	}
}
