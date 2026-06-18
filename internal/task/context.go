package task

import "path/filepath"

func ContextPath(taskDir string) string { return filepath.Join(taskDir, "context.md") }

func ContextTemplate(name, background string) string {
	return "# " + name + "\n\n" +
		"## Background\n" + background + "\n\n" +
		"## Plan\n\n" +
		"## Decisions\n\n" +
		"## Journal\n"
}
