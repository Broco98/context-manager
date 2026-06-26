package cli

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/kimhyoyeon/context-manager/internal/task"
)

// TestConcurrentWorklistAddNoLostUpdate reproduces the task.yaml write race:
// every `ctx` mutation does load -> modify -> save, so without a per-task lock two
// concurrent mutations read the same snapshot and the slower writer clobbers the
// faster one's change (last-writer-wins). N concurrent `task add` invocations must
// therefore all survive; a smaller count proves lost updates. The goroutines are
// released together via a start gate to maximize the load-to-save contention window.
func TestConcurrentWorklistAddNoLostUpdate(t *testing.T) {
	home := t.TempDir()
	seedTask(t, home, "tk")

	const n = 50
	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			<-start
			if _, err := runTaskAdd(home, "tk", fmt.Sprintf("item-%d", i)); err != nil {
				t.Errorf("runTaskAdd %d: %v", i, err)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	tk, err := task.Load(filepath.Join(home, "tk"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tk.Worklist) != n {
		t.Fatalf("lost updates: want %d worklist items, got %d", n, len(tk.Worklist))
	}
}
