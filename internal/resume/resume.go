package resume

import (
	"os"
	"strings"

	"github.com/kimhyoyeon/context-manager/internal/store"
)

// ParseSections splits markdown by "## Heading" into heading->body (trimmed).
func ParseSections(md string) map[string]string {
	m := map[string]string{}
	var cur string
	var buf []string
	flush := func() {
		if cur != "" {
			m[cur] = strings.TrimSpace(strings.Join(buf, "\n"))
		}
		buf = nil
	}
	for _, line := range strings.Split(md, "\n") {
		if strings.HasPrefix(line, "## ") {
			flush()
			cur = strings.TrimSpace(strings.TrimPrefix(line, "## "))
			continue
		}
		if cur != "" {
			buf = append(buf, line)
		}
	}
	flush()
	return m
}

func Section(m map[string]string, name string) string {
	if v, ok := m[name]; ok && strings.TrimSpace(v) != "" {
		return v
	}
	return "MISSING"
}

// JournalEntries collects the Journal section's single-line entries. Journal
// entries are single-line by contract (`- [date] msg`), so only `- ` lines are
// collected.
func JournalEntries(md string) []string {
	body := ParseSections(md)["Journal"]
	var out []string
	for _, l := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "- ") {
			out = append(out, strings.TrimSpace(l))
		}
	}
	return out
}

// AppendJournal inserts "- [date] msg" at the end of the ## Journal section and
// rewrites context.md atomically (store.AtomicWrite). It does NOT assume Journal
// is the final section: it locates the Journal heading, appends the entry after
// the last existing content line of that section, and preserves any sections
// that follow Journal. If no Journal section exists, one is appended at the end.
func AppendJournal(contextPath, dateOnly, msg string) error {
	data, err := os.ReadFile(contextPath)
	if err != nil {
		return err
	}
	entry := "- [" + dateOnly + "] " + msg
	lines := strings.Split(string(data), "\n")

	// Find the Journal heading and the start of the next "## " heading (if any).
	journalIdx := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == "## Journal" {
			journalIdx = i
			break
		}
	}
	if journalIdx < 0 {
		// No Journal section: append one at the end.
		out := strings.TrimRight(string(data), "\n")
		out += "\n\n## Journal\n" + entry + "\n"
		return store.AtomicWrite(contextPath, []byte(out))
	}
	nextIdx := len(lines)
	for i := journalIdx + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			nextIdx = i
			break
		}
	}
	// Insertion point: just after the last non-blank line within the section,
	// so the entry lands under Journal even when other sections follow.
	insertAt := journalIdx + 1
	for i := journalIdx + 1; i < nextIdx; i++ {
		if strings.TrimSpace(lines[i]) != "" {
			insertAt = i + 1
		}
	}
	out := make([]string, 0, len(lines)+1)
	out = append(out, lines[:insertAt]...)
	out = append(out, entry)
	out = append(out, lines[insertAt:]...)
	return store.AtomicWrite(contextPath, []byte(strings.Join(out, "\n")))
}
