package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/kimhyoyeon/context-manager/internal/knowledge"
	"github.com/kimhyoyeon/context-manager/internal/output"
	"github.com/kimhyoyeon/context-manager/internal/store"
	"github.com/spf13/cobra"
)

func runKnowAdd(home string, in knowledge.PageInput) (string, error) {
	if len(in.Projects) == 0 || in.Topic == "" {
		return "", output.Errorf(jsonOut, output.ErrUsage, "--project and --topic are required")
	}
	if in.SourceTask == "" {
		return "", output.Errorf(jsonOut, output.ErrUsage, "a source task is required for provenance (pass --source-task or run inside a task)")
	}
	if !knowledge.ValidCategory(in.Category) {
		return "", output.Errorf(jsonOut, output.ErrUsage, "--category must be one of architecture|decision|gotcha|pattern")
	}
	if in.When == "" {
		in.When = time.Now().UTC().Format("2006-01-02")
	}
	return knowledge.Add(home, in)
}

func runKnowSearch(home string, q knowledge.Query) ([]knowledge.Result, error) {
	return knowledge.Search(home, q)
}

func runKnowIndex(home string) (string, error) {
	data, err := os.ReadFile(store.KnowledgeDir(home) + "/index.md")
	if err != nil {
		if os.IsNotExist(err) {
			return "# Knowledge Index\n\n> 0 page(s)\n", nil
		}
		return "", err
	}
	return string(data), nil
}

func init() {
	knowCmd := &cobra.Command{Use: "know", Short: "Per-project persistent knowledge"}

	var projects, tags []string
	var topic, category, from, sourceTask string
	addCmd := &cobra.Command{
		Use: "add", Short: "Create or merge a topic page",
		RunE: func(_ *cobra.Command, _ []string) error {
			home, err := store.Home()
			if err != nil {
				return err
			}
			// Provenance: explicit --source-task wins, else resolve the active
			// task from CWD so consolidation always records a non-empty source.
			src := sourceTask
			if src == "" {
				if cwd, cerr := os.Getwd(); cerr == nil {
					if loc, lerr := store.Locate(home, cwd); lerr == nil {
						src = loc.Task
					}
				}
			}
			body := ""
			if from != "" {
				b, rerr := os.ReadFile(from)
				if rerr != nil {
					return rerr
				}
				body = string(b)
			} else {
				// Stat the stdin handle defensively: if stdin is closed or Stat
				// fails, fi is nil and fi.Mode() would panic, breaking the
				// structured-error contract. Surface the error for centralized
				// rendering instead.
				fi, serr := os.Stdin.Stat()
				if serr != nil {
					return output.Errorf(jsonOut, output.ErrUsage, "cannot inspect stdin: %v", serr)
				}
				if fi == nil {
					return output.Errorf(jsonOut, output.ErrUsage, "cannot inspect stdin: no file info")
				}
				if (fi.Mode() & os.ModeCharDevice) == 0 {
					b, rerr := io.ReadAll(os.Stdin)
					if rerr != nil {
						return output.Errorf(jsonOut, output.ErrUsage, "cannot read stdin: %v", rerr)
					}
					body = string(b)
				}
			}
			path, err := runKnowAdd(home, knowledge.PageInput{
				Projects: projects, Topic: topic, Category: category, Tags: tags,
				SourceTask: src, Body: strings.TrimSpace(body),
			})
			if err != nil {
				return err
			}
			return output.Emit(jsonOut, "wrote "+path, map[string]string{"path": path})
		},
	}
	addCmd.Flags().StringSliceVar(&projects, "project", nil, "project(s) this knowledge belongs to (≥2 → _shared)")
	addCmd.Flags().StringVar(&topic, "topic", "", "topic name (the compounding key)")
	addCmd.Flags().StringVar(&category, "category", "", "architecture|decision|gotcha|pattern")
	addCmd.Flags().StringSliceVar(&tags, "tags", nil, "comma-separated tags")
	addCmd.Flags().StringVar(&from, "from", "", "read body from file (else stdin)")
	addCmd.Flags().StringVar(&sourceTask, "source-task", "", "originating task for provenance (default: detect from CWD)")

	var sProject, sTag, sCategory string
	searchCmd := &cobra.Command{
		Use: "search <query>", Short: "Search topic pages (keyword + facets)", Args: cobra.MinimumNArgs(0),
		RunE: func(_ *cobra.Command, args []string) error {
			home, err := store.Home()
			if err != nil {
				return err
			}
			res, err := runKnowSearch(home, knowledge.Query{
				Text: strings.Join(args, " "), Project: sProject, Tag: sTag, Category: sCategory,
			})
			if err != nil {
				return err
			}
			human := fmt.Sprintf("%d result(s)", len(res))
			for _, r := range res {
				human += fmt.Sprintf("\n  [%d] %s (%s) — %s", r.Score, r.Topic, strings.Join(r.Project, ","), r.Snippet)
			}
			return output.Emit(jsonOut, human, map[string]any{"results": res})
		},
	}
	searchCmd.Flags().StringVar(&sProject, "project", "", "filter by project")
	searchCmd.Flags().StringVar(&sTag, "tag", "", "filter by tag")
	searchCmd.Flags().StringVar(&sCategory, "category", "", "filter by category")

	indexCmd := &cobra.Command{
		Use: "index", Short: "Print the knowledge catalog (read this first)",
		RunE: func(_ *cobra.Command, _ []string) error {
			home, err := store.Home()
			if err != nil {
				return err
			}
			idx, err := runKnowIndex(home)
			if err != nil {
				return err
			}
			return output.Emit(jsonOut, idx, map[string]string{"index": idx})
		},
	}

	knowCmd.AddCommand(addCmd, searchCmd, indexCmd)
	RootCmd.AddCommand(knowCmd)
}
