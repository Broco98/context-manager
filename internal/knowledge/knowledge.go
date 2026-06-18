package knowledge

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/kimhyoyeon/context-manager/internal/store"
	"gopkg.in/yaml.v3"
)

type Source struct {
	Task string `yaml:"task" json:"task"`
	When string `yaml:"when" json:"when"`
	PR   string `yaml:"pr,omitempty" json:"pr,omitempty"`
}
type Page struct {
	Project      []string `yaml:"project" json:"project"`
	Category     string   `yaml:"category,omitempty" json:"category,omitempty"`
	Topic        string   `yaml:"topic" json:"topic"`
	Status       string   `yaml:"status" json:"status"`
	Tags         []string `yaml:"tags,omitempty" json:"tags,omitempty"`
	Sources      []Source `yaml:"sources,omitempty" json:"sources,omitempty"`
	LastReviewed string   `yaml:"last_reviewed,omitempty" json:"last_reviewed,omitempty"`
	Body         string   `yaml:"-" json:"-"`
}
type PageInput struct {
	Projects   []string
	Topic      string
	Category   string
	Body       string
	SourceTask string
	When       string
	Tags       []string
}

func Slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	dash := false
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
			dash = false
		} else if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func Render(p *Page) []byte {
	fm, _ := yaml.Marshal(p)
	return []byte("---\n" + string(fm) + "---\n\n" + strings.TrimSpace(p.Body) + "\n")
}

func ParsePage(data []byte) (*Page, error) {
	s := string(data)
	if !strings.HasPrefix(s, "---\n") {
		return &Page{Body: s}, nil
	}
	rest := s[4:]
	i := strings.Index(rest, "\n---")
	if i < 0 {
		return &Page{Body: s}, nil
	}
	var p Page
	if err := yaml.Unmarshal([]byte(rest[:i]), &p); err != nil {
		return nil, err
	}
	p.Body = strings.TrimLeft(rest[i+4:], "\n")
	return &p, nil
}

func union(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range append(append([]string{}, a...), b...) {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func targetDir(home string, projects []string) string {
	if len(projects) >= 2 {
		return store.SharedDir(home)
	}
	return filepath.Join(store.KnowledgeDir(home), projects[0])
}

// pageAt loads a single topic page at a specific group directory, if present.
func pageAt(home, group, topicSlug string) (string, *Page) {
	cand := filepath.Join(store.KnowledgeDir(home), group, topicSlug+".md")
	if data, err := os.ReadFile(cand); err == nil {
		if p, perr := ParsePage(data); perr == nil {
			return cand, p
		}
	}
	return "", nil
}

// scopedMatch is one existing page found within the declared scope for a topic.
type scopedMatch struct {
	Path string
	Page *Page
}

// findScopedPages collects EVERY existing page for this topic within the scope
// declared by the input projects. It considers EACH named <project>/ directory,
// and _shared/ ONLY when the existing shared page's Project facet actually
// overlaps the requested projects. Without that overlap check, adding topic
// "pay" for project "cli" would wrongly merge an existing _shared/pay.md owned
// only by "front"+"back" (SPEC §9 per-project isolation / shared-page scope): a
// shared page belongs to the repos it lists, not to every project. When there is
// no overlap, _shared/ is skipped so the requested project gets (or merges into)
// its OWN independent <project>/ page instead. Returning all in-scope matches
// also lets an explicit multi-project add merge AND supersede every per-project
// page that already holds the topic — e.g. both front/pay.md and back/pay.md.
// Matches are returned _shared first (when in scope), then in the projects order,
// deduplicated by path.
func findScopedPages(home string, projects []string, topicSlug string) []scopedMatch {
	var matches []scopedMatch
	seen := map[string]bool{}
	add := func(group string) {
		if path, p := pageAt(home, group, topicSlug); p != nil && !seen[path] {
			seen[path] = true
			matches = append(matches, scopedMatch{Path: path, Page: p})
		}
	}
	// Include the shared page only when its facet overlaps the requested scope.
	if sharedPath, shared := pageAt(home, "_shared", topicSlug); shared != nil {
		want := map[string]bool{}
		for _, proj := range projects {
			want[proj] = true
		}
		overlaps := false
		for _, owner := range shared.Project {
			if want[owner] {
				overlaps = true
				break
			}
		}
		if overlaps && !seen[sharedPath] {
			seen[sharedPath] = true
			matches = append(matches, scopedMatch{Path: sharedPath, Page: shared})
		}
	}
	for _, proj := range projects {
		add(proj)
	}
	return matches
}

// ValidCategory reports whether c is empty (category is optional) or one of the
// controlled vocabulary values architecture|decision|gotcha|pattern.
func ValidCategory(c string) bool {
	switch c {
	case "", "architecture", "decision", "gotcha", "pattern":
		return true
	default:
		return false
	}
}

func Add(home string, in PageInput) (string, error) {
	if len(in.Projects) == 0 {
		return "", fmt.Errorf("at least one project is required")
	}
	if strings.TrimSpace(in.SourceTask) == "" {
		return "", fmt.Errorf("a non-empty source task is required for provenance")
	}
	if !ValidCategory(in.Category) {
		return "", fmt.Errorf("category must be one of architecture|decision|gotcha|pattern, got %q", in.Category)
	}
	// Each project name becomes a knowledge group directory, so reject names that
	// could escape _knowledge/ (e.g. "../front") or collide with the reserved
	// _shared/_knowledge dirs (underscore-prefixed names).
	for _, proj := range in.Projects {
		if err := store.ValidName(proj); err != nil {
			return "", fmt.Errorf("invalid project %q: %w", proj, err)
		}
	}
	topicSlug := Slug(in.Topic)
	matches := findScopedPages(home, in.Projects, topicSlug)

	var page *Page
	if len(matches) > 0 {
		// Merge EVERY in-scope page (e.g. front/pay.md AND back/pay.md) into one,
		// unioning facets/tags/sources and concatenating bodies, so an explicit
		// multi-project promotion never leaves a stale per-project duplicate.
		page = matches[0].Page
		for _, m := range matches[1:] {
			page.Project = union(page.Project, m.Page.Project)
			page.Tags = union(page.Tags, m.Page.Tags)
			page.Sources = append(page.Sources, m.Page.Sources...)
			if page.Category == "" && m.Page.Category != "" {
				page.Category = m.Page.Category
			}
			if body := strings.TrimSpace(m.Page.Body); body != "" {
				page.Body = strings.TrimSpace(page.Body) + "\n\n" + body
			}
			if m.Page.LastReviewed > page.LastReviewed {
				page.LastReviewed = m.Page.LastReviewed
			}
		}
		page.Project = union(page.Project, in.Projects)
		page.Tags = union(page.Tags, in.Tags)
		if in.Category != "" {
			page.Category = in.Category
		}
		page.Body = strings.TrimSpace(page.Body) + "\n\n## Update (" + in.When + ")\n" + strings.TrimSpace(in.Body)
		page.Sources = append(page.Sources, Source{Task: in.SourceTask, When: in.When})
		page.LastReviewed = in.When
	} else {
		page = &Page{
			Project: union(in.Projects, nil), Category: in.Category, Topic: in.Topic,
			Status: "active", Tags: union(in.Tags, nil),
			Sources: []Source{{Task: in.SourceTask, When: in.When}}, LastReviewed: in.When,
			Body: in.Body,
		}
	}

	// Promotion to _shared happens only when the resulting facet spans ≥2 repos.
	newPath := filepath.Join(targetDir(home, page.Project), topicSlug+".md")
	// Canonicalize the write target against CTX_HOME itself so a symlinked group
	// dir — or a symlinked _knowledge dir — redirecting outside CTX_HOME cannot
	// redirect the write or the supersede-deletions below to a real location off
	// the store. Anchoring to KnowledgeDir would resolve a symlinked _knowledge to
	// its real (possibly off-store) location and validate the target against that
	// wrong base, so we anchor to home and let ValidRelPath resolve both ends.
	homeRel, rerr := filepath.Rel(home, newPath)
	if rerr != nil {
		return "", rerr
	}
	if _, verr := store.ValidRelPath(home, homeRel); verr != nil {
		return "", verr
	}
	// Write the (possibly promoted) page FIRST so a failure never loses any source
	// page; only after a successful write do we remove EVERY superseded per-project
	// page so no duplicate of the topic survives.
	if err := store.AtomicWrite(newPath, Render(page)); err != nil {
		return "", err
	}
	for _, m := range matches {
		if m.Path == newPath {
			continue
		}
		// Validate EVERY deletion target against CTX_HOME immediately before the
		// os.Remove: a symlinked per-project group dir could otherwise make this
		// delete a file outside the store. Anchor to home (not KnowledgeDir) for the
		// same reason as newPath above, and let ValidRelPath resolve symlinks on both
		// ends so a redirected source page is rejected rather than removed off-store.
		mRel, mErr := filepath.Rel(home, m.Path)
		if mErr != nil {
			return "", mErr
		}
		if _, verr := store.ValidRelPath(home, mRel); verr != nil {
			return "", verr
		}
		if err := os.Remove(m.Path); err != nil && !os.IsNotExist(err) {
			return "", err
		}
	}
	if err := updateIndex(home); err != nil {
		return "", err
	}
	if err := appendLog(home, "## ["+in.When+"] consolidate | "+page.Topic); err != nil {
		return "", err
	}
	return newPath, nil
}

// HasProvenance reports whether a knowledge page consolidates the given task's
// findings for one of its registered projects. A page qualifies only when it both
// (1) records taskName as a sources: entry AND (2) has a project facet that
// overlaps taskProjects. Requiring the overlap implements SPEC §7/§9: `ctx done`
// must verify the task's *relevant per-project* findings were consolidated, so a
// page written for an unrelated project (e.g. provenance for "back" when the task
// only registered "front") never satisfies the gate. When taskProjects is empty
// the task registered no projects, so there are no per-project findings to require
// and any same-task page qualifies.
func HasProvenance(home, taskName string, taskProjects []string) (bool, error) {
	if strings.TrimSpace(taskName) == "" {
		return false, fmt.Errorf("task name is required")
	}
	want := map[string]bool{}
	for _, p := range taskProjects {
		if p != "" {
			want[p] = true
		}
	}
	for _, ref := range listPages(home) {
		sourced := false
		for _, s := range ref.Page.Sources {
			if s.Task == taskName {
				sourced = true
				break
			}
		}
		if !sourced {
			continue
		}
		if len(want) == 0 {
			return true, nil
		}
		for _, proj := range ref.Page.Project {
			if want[proj] {
				return true, nil
			}
		}
	}
	return false, nil
}

type pageRef struct {
	Group string
	Path  string
	Page  *Page
}

func listPages(home string) []pageRef {
	base := store.KnowledgeDir(home)
	groups, _ := os.ReadDir(base)
	var refs []pageRef
	for _, g := range groups {
		if !g.IsDir() {
			continue
		}
		files, _ := os.ReadDir(filepath.Join(base, g.Name()))
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), ".md") {
				continue
			}
			p := filepath.Join(base, g.Name(), f.Name())
			if data, err := os.ReadFile(p); err == nil {
				if pg, perr := ParsePage(data); perr == nil {
					refs = append(refs, pageRef{Group: g.Name(), Path: p, Page: pg})
				}
			}
		}
	}
	return refs
}

func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(strings.TrimPrefix(l, "#"))
		if l != "" {
			// Truncate by RUNES, not bytes: slicing a UTF-8 string at byte 80 can
			// cut inside a multibyte rune (Korean/Japanese/Chinese), producing
			// invalid UTF-8 in the index and search snippets.
			r := []rune(l)
			if len(r) > 80 {
				return string(r[:80])
			}
			return l
		}
	}
	return ""
}

func updateIndex(home string) error {
	refs := listPages(home)
	byGroup := map[string][]pageRef{}
	var groups []string
	for _, r := range refs {
		if _, ok := byGroup[r.Group]; !ok {
			groups = append(groups, r.Group)
		}
		byGroup[r.Group] = append(byGroup[r.Group], r)
	}
	sort.Strings(groups)
	var b strings.Builder
	fmt.Fprintf(&b, "# Knowledge Index\n\n> %d page(s)\n", len(refs))
	for _, g := range groups {
		fmt.Fprintf(&b, "\n## %s\n", g)
		rs := byGroup[g]
		sort.Slice(rs, func(i, j int) bool { return rs[i].Page.Topic < rs[j].Page.Topic })
		for _, r := range rs {
			rel := filepath.Join(g, filepath.Base(r.Path))
			fmt.Fprintf(&b, "- [%s](%s) — %s\n", r.Page.Topic, rel, firstLine(r.Page.Body))
		}
	}
	return store.AtomicWrite(filepath.Join(store.KnowledgeDir(home), "index.md"), []byte(b.String()))
}

func appendLog(home, line string) error {
	p := filepath.Join(store.KnowledgeDir(home), "log.md")
	// Validate log.md against CTX_HOME before touching it: an existing log.md symlink
	// could otherwise redirect the append (or the create below) to a file outside the
	// store. ValidRelPath resolves symlinks on both ends, so a redirected log is
	// rejected here rather than written off-store.
	if _, verr := store.ValidRelPath(home, filepath.Join("_knowledge", "log.md")); verr != nil {
		return verr
	}
	// Read-modify-write via AtomicWrite (temp file + rename) instead of O_APPEND on the
	// path: the rename replaces log.md atomically without ever following it as a symlink,
	// so the append cannot land outside CTX_HOME even if log.md is later re-symlinked.
	prior, err := os.ReadFile(p)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		prior = []byte("# Knowledge Log\n\n")
	}
	return store.AtomicWrite(p, append(prior, []byte(line+"\n")...))
}
