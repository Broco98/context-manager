package knowledge

import "testing"

func TestTokenizeKoreanProducesBigrams(t *testing.T) {
	toks := tokenize("결제처리")
	want := map[string]bool{"결": true, "제": true, "처": true, "리": true, "결제": true, "제처": true, "처리": true}
	for w := range want {
		if !hasTok(toks, w) {
			t.Errorf("missing token %q in %v", w, toks)
		}
	}
}

func TestSearchKoreanBigramMatch(t *testing.T) {
	home := t.TempDir()
	if _, err := Add(home, PageInput{Projects: []string{"back"}, Topic: "pay endpoint", Body: "결제 처리 시 200 먼저 반환", SourceTask: "t", When: "2026-06-18"}); err != nil {
		t.Fatal(err)
	}
	res, err := Search(home, Query{Text: "결제"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res) == 0 {
		t.Fatal("expected a match for 결제")
	}
}

func TestSearchProjectFilter(t *testing.T) {
	home := t.TempDir()
	_, _ = Add(home, PageInput{Projects: []string{"front"}, Topic: "routing", Body: "uses next router", SourceTask: "t", When: "2026-06-18"})
	_, _ = Add(home, PageInput{Projects: []string{"back"}, Topic: "db", Body: "uses postgres", SourceTask: "t", When: "2026-06-18"})
	res, _ := Search(home, Query{Text: "uses", Project: "front"})
	if len(res) != 1 || res[0].Topic != "routing" {
		t.Errorf("project filter failed: %+v", res)
	}
}

func hasTok(toks []string, w string) bool {
	for _, t := range toks {
		if t == w {
			return true
		}
	}
	return false
}
