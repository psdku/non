package app

import (
	"testing"

	"non/internal/filter"
	"non/internal/meta"
)

// queryLib builds a library with three tagged papers.
func queryLib(t *testing.T) *Library {
	t.Helper()
	dir := makeDir(t, "attention.pdf", "gpt3.pdf", "vllm.pdf", "plain.pdf")
	lib, err := OpenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Every tag a paper uses must be in the table, or it is invisible to both
	// the display and the filter.
	for _, tag := range []string{"attention", "LLM", "DB"} {
		lib.Store.Config.SetTag(-1, meta.Tag{Name: tag})
	}
	lib.Store.Set("attention.pdf", &meta.Entry{
		Title: "Attention Is All You Need", Author: "Vaswani",
		Tags: []string{"attention", "LLM"},
	})
	lib.Store.Set("gpt3.pdf", &meta.Entry{
		Title: "Language Models are Few-Shot Learners", Author: "Brown",
		Tags: []string{"LLM"},
	})
	lib.Store.Set("vllm.pdf", &meta.Entry{
		Title: "Efficient Memory Management with PagedAttention", Author: "Kwon",
		Tags: []string{"LLM", "DB"}, Abstract: "paged attention for serving",
	})
	lib.Store.Set("plain.pdf", &meta.Entry{Title: "Untagged Work"})
	if err := lib.Rescan(); err != nil {
		t.Fatal(err)
	}
	return lib
}

func selectedNames(lib *Library, idx []int) []string {
	out := make([]string, len(idx))
	for i, n := range idx {
		out[i] = lib.Papers[n].Name
	}
	return out
}

func mustParse(t *testing.T, expr string) filter.Node {
	t.Helper()
	n, err := filter.Parse(expr)
	if err != nil {
		t.Fatalf("Parse(%q): %v", expr, err)
	}
	return n
}

func TestQueryEmptyKeepsEverything(t *testing.T) {
	lib := queryLib(t)
	q := Query{}
	if !q.Empty() {
		t.Error("an empty query should report Empty")
	}
	if got := q.Select(lib); len(got) != len(lib.Papers) {
		t.Errorf("empty query selected %d of %d", len(got), len(lib.Papers))
	}
}

func TestQueryByTag(t *testing.T) {
	lib := queryLib(t)
	got := selectedNames(lib, Query{Tags: mustParse(t, "LLM")}.Select(lib))
	if len(got) != 3 {
		t.Errorf("LLM = %v, want 3 papers", got)
	}
	got = selectedNames(lib, Query{Tags: mustParse(t, "LLM && DB")}.Select(lib))
	if len(got) != 1 || got[0] != "vllm.pdf" {
		t.Errorf("LLM && DB = %v", got)
	}
	got = selectedNames(lib, Query{Tags: mustParse(t, "LLM && !DB")}.Select(lib))
	if len(got) != 2 {
		t.Errorf("LLM && !DB = %v", got)
	}
	// Case-insensitive, like the TUI.
	if got := (Query{Tags: mustParse(t, "llm")}).Select(lib); len(got) != 3 {
		t.Errorf("lowercase llm = %v", selectedNames(lib, got))
	}
	// A tag nobody has selects nothing, rather than everything.
	if got := (Query{Tags: mustParse(t, "nosuchtag")}).Select(lib); len(got) != 0 {
		t.Errorf("unknown tag = %v", selectedNames(lib, got))
	}
	// A paper with no metadata is not excluded by a negative-only filter.
	if got := selectedNames(lib, Query{Tags: mustParse(t, "!DB")}.Select(lib)); len(got) != 3 {
		t.Errorf("!DB = %v, want 3 (plain has no tags)", got)
	}
}

func TestQueryText(t *testing.T) {
	lib := queryLib(t)
	got := selectedNames(lib, Query{Text: "vllm"}.Select(lib))
	if len(got) != 1 || got[0] != "vllm.pdf" {
		t.Errorf("vllm = %v", got)
	}
	// The title is always searchable, even without metadata search.
	got = selectedNames(lib, Query{Text: "attention"}.Select(lib))
	if len(got) == 0 {
		t.Error("title should match without metadata search")
	}
	// The abstract only matches with WithMeta.
	with := Query{Text: "serving", WithMeta: true}.Select(lib)
	without := Query{Text: "serving"}.Select(lib)
	if len(with) == 0 {
		t.Error("abstract should match with WithMeta")
	}
	if len(without) != 0 {
		t.Errorf("abstract matched without WithMeta: %v", selectedNames(lib, without))
	}
}

func TestQueryCombinesTagAndText(t *testing.T) {
	lib := queryLib(t)
	// "Attention" appears in two titles; the tag narrows it to one.
	got := selectedNames(lib, Query{Tags: mustParse(t, "DB"), Text: "attention"}.Select(lib))
	if len(got) != 1 || got[0] != "vllm.pdf" {
		t.Errorf("DB + attention = %v", got)
	}
	// Indices point at the right papers: this is the mapping bug that would
	// otherwise return the right count with the wrong papers.
	q := Query{Tags: mustParse(t, "DB")}
	for _, i := range q.Select(lib) {
		if !HasTag(lib.Papers[i], "DB") {
			t.Errorf("index %d is %s, which has no DB tag", i, lib.Papers[i].Name)
		}
	}
}

func TestQueryOrderIsStable(t *testing.T) {
	lib := queryLib(t)
	tags := Query{Tags: mustParse(t, "LLM")}
	first := selectedNames(lib, tags.Select(lib))
	for range 5 {
		again := selectedNames(lib, tags.Select(lib))
		for j := range first {
			if first[j] != again[j] {
				t.Fatalf("order changed between runs: %v vs %v", first, again)
			}
		}
	}
}

func TestHasTag(t *testing.T) {
	p := &Paper{Meta: &meta.Entry{Tags: []string{"LLM"}}}
	if !HasTag(p, "llm") {
		t.Error("HasTag should ignore case")
	}
	if HasTag(p, "DB") {
		t.Error("false positive")
	}
	if HasTag(&Paper{}, "LLM") {
		t.Error("a paper without metadata has no tags")
	}
}
