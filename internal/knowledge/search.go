package knowledge

import (
	"sort"
	"strings"
	"unicode"
)

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hangul, r) ||
		unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r)
}

// tokenize lowercases, splits latin/numeric on non-alnum, and emits CJK
// unigrams + adjacent bigrams (so "결제" matches inside "결제처리").
func tokenize(s string) []string {
	s = strings.ToLower(s)
	var toks []string
	var latin, cjk []rune
	flushLatin := func() {
		if len(latin) > 0 {
			toks = append(toks, string(latin))
			latin = nil
		}
	}
	flushCJK := func() {
		for _, r := range cjk {
			toks = append(toks, string(r))
		}
		for i := 0; i+1 < len(cjk); i++ {
			toks = append(toks, string(cjk[i:i+2]))
		}
		cjk = nil
	}
	for _, r := range s {
		switch {
		case isCJK(r):
			flushLatin()
			cjk = append(cjk, r)
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			flushCJK()
			latin = append(latin, r)
		default:
			flushLatin()
			flushCJK()
		}
	}
	flushLatin()
	flushCJK()
	return toks
}

type Query struct {
	Text, Project, Tag, Category string
}
type Result struct {
	Path    string   `json:"path"`
	Topic   string   `json:"topic"`
	Score   int      `json:"score"`
	Snippet string   `json:"snippet"`
	Project []string `json:"project"`
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

func countTok(toks []string, term string) int {
	n := 0
	for _, t := range toks {
		if t == term {
			n++
		}
	}
	return n
}

func Search(home string, q Query) ([]Result, error) {
	qtokens := tokenize(q.Text)
	var results []Result
	for _, ref := range listPages(home) {
		p := ref.Page
		if q.Project != "" && !contains(p.Project, q.Project) {
			continue
		}
		if q.Category != "" && p.Category != q.Category {
			continue
		}
		if q.Tag != "" && !contains(p.Tags, q.Tag) {
			continue
		}
		score := 0
		if q.Text == "" {
			score = 1
		} else {
			titleToks := tokenize(p.Topic)
			bodyToks := tokenize(p.Body)
			for _, qt := range qtokens {
				// This keyword tag-boost matches single-token tags only; exact
				// multi-word tag matching is handled by the --tag facet filter above.
				if contains(p.Tags, qt) {
					score += 3
				}
				score += 2 * countTok(titleToks, qt)
				score += 2 * countTok(bodyToks, qt)
			}
		}
		if score > 0 {
			results = append(results, Result{
				Path: ref.Path, Topic: p.Topic, Score: score,
				Snippet: firstLine(p.Body), Project: p.Project,
			})
		}
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Score != results[j].Score {
			return results[i].Score > results[j].Score
		}
		return results[i].Topic < results[j].Topic
	})
	return results, nil
}
