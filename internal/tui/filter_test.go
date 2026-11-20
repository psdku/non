package tui

import (
	"strings"
	"testing"

	"non/internal/meta"
)

// filterModel builds a model with a tag table and tagged papers.
func filterModel(t *testing.T) *Model {
	t.Helper()
	m := newTestModel(t, "attention.pdf", "gpt3.pdf", "vllm.pdf")
	c := &m.lib.Store.Config
	c.TagTable = []meta.Tag{
		{Name: "LLM", Description: "large language models"},
		{Name: "DB", Description: "databases"},
		{Name: "attention", Description: "attention mechanisms"},
	}
	m.lib.Store.Set("attention.pdf", &meta.Entry{Title: "Attention", Tags: []string{"attention"}})
	m.lib.Store.Set("gpt3.pdf", &meta.Entry{Title: "GPT-3", Tags: []string{"LLM"}})
	m.lib.Store.Set("vllm.pdf", &meta.Entry{Title: "vLLM", Tags: []string{"LLM", "DB"}})
	if err := m.lib.Rescan(); err != nil {
		t.Fatal(err)
	}
	m.rebuild()
	return m
}

// names returns the visible file names in order.
func names(m *Model) []string {
	out := make([]string, 0, len(m.visible))
	for _, i := range m.visible {
		out = append(out, m.all[i].Name)
	}
	return out
}

func TestFilterNarrows(t *testing.T) {
	cases := []struct {
		expr string
		want []string
	}{
		{"", []string{"attention.pdf", "gpt3.pdf", "vllm.pdf"}},
		{"LLM", []string{"gpt3.pdf", "vllm.pdf"}},
		{"!LLM", []string{"attention.pdf"}},
		{"LLM && DB", []string{"vllm.pdf"}},
		{"!LLM && DB", []string{}}, // the only DB paper is also LLM
		{"LLM || attention", []string{"attention.pdf", "gpt3.pdf", "vllm.pdf"}},
		{"attention and not DB", []string{"attention.pdf"}},
		{"llm", []string{"gpt3.pdf", "vllm.pdf"}}, // case-insensitive
		{"LLM DB", []string{"vllm.pdf"}},          // implicit AND
	}
	for _, c := range cases {
		m := filterModel(t)
		m.setFilterText(c.expr)
		if m.filterErr != nil {
			t.Errorf("%q: %v", c.expr, m.filterErr)
			continue
		}
		got := names(m)
		if strings.Join(got, ",") != strings.Join(c.want, ",") {
			t.Errorf("%q = %v, want %v", c.expr, got, c.want)
		}
	}
}

func TestFilterIsLive(t *testing.T) {
	m := filterModel(t)
	m.mode = modeFilter
	m.fin.Focus()
	for _, r := range "LLM" {
		m = key(m, string(r))
	}
	if got := len(m.visible); got != 2 {
		t.Errorf("after typing LLM: %d visible, want 2", got)
	}
}

func TestFilterIncompleteKeepsLastTree(t *testing.T) {
	// A half-typed expression must not empty the list.
	m := filterModel(t)
	m.setFilterText("LLM")
	before := names(m)
	m.setFilterText("LLM &&")
	if m.filterErr == nil {
		t.Fatal("expected a parse error for LLM &&")
	}
	if got := names(m); strings.Join(got, ",") != strings.Join(before, ",") {
		t.Errorf("visible changed on a bad expression: %v", got)
	}
	// Once it parses again, the new tree takes over.
	m.setFilterText("LLM && DB")
	if got := names(m); strings.Join(got, ",") != "vllm.pdf" {
		t.Errorf("after fixing = %v", got)
	}
}

func TestFilterComposesWithSearch(t *testing.T) {
	// The filter narrows the set, then the fuzzy query ranks what is left.
	m := filterModel(t)
	m.setFilterText("LLM")
	m.quat = "vllm"
	m.rebuild()
	got := names(m)
	if len(got) != 1 || got[0] != "vllm.pdf" {
		t.Fatalf("filter+search = %v, want [vllm.pdf]", got)
	}
	// The same query without the filter also matches gpt3 (its abstract would
	// only match with metadata search, so use a name query instead).
	m.setFilterText("")
	m.quat = "gpt"
	m.rebuild()
	if len(m.visible) != 1 {
		t.Errorf("search alone = %v", names(m))
	}
}

func TestFilterKeysClearIndependently(t *testing.T) {
	m := filterModel(t)
	m.setFilterText("LLM")
	m.quat = "vllm"
	m.rebuild()

	// esc in browse clears the search first.
	m = key(m, "esc")
	if m.quat != "" {
		t.Errorf("search survived esc: %q", m.quat)
	}
	if !m.filterActive() {
		t.Error("esc cleared the filter as well as the search")
	}
	// A second esc clears the filter.
	m = key(m, "esc")
	if m.filterActive() || m.filterText != "" {
		t.Errorf("filter survived the second esc: %q", m.filterText)
	}
	if got := len(m.visible); got != 3 {
		t.Errorf("visible = %d after clearing everything, want 3", got)
	}
}

func TestFilterEnterKeepsEscClears(t *testing.T) {
	m := filterModel(t)
	m.mode = modeFilter
	m.fin.Focus()
	m.fin.SetValue("LLM")
	m.setFilterText("LLM")
	m = key(m, "enter")
	if m.mode != modeBrowse {
		t.Errorf("mode = %v after enter", m.mode)
	}
	if !m.filterActive() {
		t.Error("enter should keep the filter")
	}
	// Reopening the prompt shows the current expression.
	m = key(m, "f")
	if m.fin.Value() != "LLM" {
		t.Errorf("prompt value = %q", m.fin.Value())
	}
	m = key(m, "esc")
	if m.filterActive() || m.filterText != "" {
		t.Error("esc in the prompt should clear the filter")
	}
}

func TestFilterUnknownTagWarns(t *testing.T) {
	m := filterModel(t)
	m.setFilterText("LLM && nosuchtag")
	if m.filterErr != nil {
		t.Fatal(m.filterErr)
	}
	unknown := m.filterUnknown()
	if strings.Join(unknown, ",") != "nosuchtag" {
		t.Errorf("filterUnknown = %v", unknown)
	}
	// A known tag in another case is not unknown.
	m.setFilterText("llm")
	if got := m.filterUnknown(); len(got) != 0 {
		t.Errorf("filterUnknown = %v for a known tag", got)
	}
}

func TestFilterEmptyResultStillRenders(t *testing.T) {
	m := filterModel(t)
	m.width, m.height = 80, 20
	m.setFilterText("LLM && DB && attention")
	out := m.View()
	if !strings.Contains(out, "no matching papers") {
		t.Errorf("empty filter result not explained:\n%s", out)
	}
	if !strings.Contains(stripEsc(out), "filter 0/3") {
		t.Errorf("status bar does not show the filter state:\n%s", stripEsc(out))
	}
}

func TestFilterShownInHeader(t *testing.T) {
	m := filterModel(t)
	m.width, m.height = 100, 24
	m.setFilterText("LLM && !DB")
	header := stripEsc(strings.Split(m.View(), "\n")[0])
	if !strings.Contains(header, "[ LLM && !DB ]") {
		t.Errorf("header = %q, want the active filter", header)
	}
}

func TestFilterUntaggedPaperIsExcluded(t *testing.T) {
	// A paper with no metadata at all cannot satisfy any tag test, and must not
	// panic while being evaluated.
	m := filterModel(t)
	m.lib.Store.Delete("attention.pdf")
	if err := m.lib.Rescan(); err != nil {
		t.Fatal(err)
	}
	m.setFilterText("LLM")
	if got := names(m); strings.Join(got, ",") != "gpt3.pdf,vllm.pdf" {
		t.Errorf("visible = %v", got)
	}
	m.setFilterText("!LLM")
	if got := names(m); len(got) != 1 || got[0] != "attention.pdf" {
		t.Errorf("!LLM = %v, want the untagged paper", got)
	}
}
