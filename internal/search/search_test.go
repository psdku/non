package search

import (
	"encoding/json"
	"strings"
	"testing"

	"non/internal/meta"
)

func docs(specs ...string) []Doc {
	// Each spec is "name|title|author|tags".
	out := make([]Doc, len(specs))
	for i, s := range specs {
		// Missing trailing parts are allowed, so a short spec works too.
		parts := append(strings.Split(s, "|"), "", "", "")
		e := &meta.Entry{Title: parts[1], Author: parts[2]}
		if parts[3] != "" {
			e.Tags = strings.Split(parts[3], ",")
		}
		out[i] = Doc{Name: parts[0], Meta: e}
	}
	return out
}

// A name hit outranks an identical author hit: this is the tiering the TUI
// relies on to put the paper you named first.
func TestRankingTiers(t *testing.T) {
	d := docs(
		"unrelated.pdf|A Study|paged attention and memory|",
		"paged.pdf|Something Else|Nobody|",
	)
	got := OrderOf("paged", d, nil, true)
	if len(got) != 2 {
		t.Fatalf("got %d hits, want 2", len(got))
	}
	if got[0] != 1 {
		t.Errorf("the file name hit should rank first, got order %v", got)
	}
}

// Without metadata search, an author match is not a hit at all.
func TestRankingRespectsWithMeta(t *testing.T) {
	d := docs("a.pdf|Title|Paged Attention Author|")
	if got := OrderOf("paged", d, nil, false); len(got) != 0 {
		t.Errorf("author matched without WithMeta: %v", got)
	}
	if got := OrderOf("paged", d, nil, true); len(got) != 1 {
		t.Errorf("author missed with WithMeta: %v", got)
	}
}

// Titles are always searchable: the list shows them, so a query must find them.
func TestRankingAlwaysMatchesTitle(t *testing.T) {
	d := docs("a.pdf|Attention Is All You Need|Vaswani|")
	if got := OrderOf("attention", d, nil, false); len(got) != 1 {
		t.Errorf("title not matched: %v", got)
	}
}

// The body fields only match with metadata search, as substrings.
func TestRankingBody(t *testing.T) {
	d := docs("a.pdf|Title|Author|")
	d[0].Meta.Abstract = "we describe a paged attention scheme"
	if got := OrderOf("paged", d, nil, false); len(got) != 0 {
		t.Errorf("abstract matched without WithMeta: %v", got)
	}
	if got := OrderOf("paged", d, nil, true); len(got) != 1 {
		t.Errorf("abstract not matched with WithMeta: %v", got)
	}
	// Extra fields count as body text too.
	d[0].Meta.Abstract = ""
	d[0].Meta.SetRawField("note", json.RawMessage(`"a paged scheme"`))
	if got := OrderOf("paged", d, nil, true); len(got) != 1 {
		t.Errorf("extra field not matched: %v", got)
	}
}

// A nil positions slice means identity, and a nil/given mapping is applied
// exactly once.
func TestRankingPositions(t *testing.T) {
	d := docs("a.pdf|Alpha|", "b.pdf|Beta|")
	got := OrderOf("alpha", d, nil, false)
	if len(got) != 1 || got[0] != 0 {
		t.Errorf("identity mapping = %v", got)
	}
	got = OrderOf("alpha", d, []int{10, 11}, false)
	if len(got) != 1 || got[0] != 10 {
		t.Errorf("explicit mapping = %v", got)
	}
}

// The file name breaks ties, so equal scores keep a deterministic order.
func TestRankingIsDeterministic(t *testing.T) {
	d := docs("b.pdf|same|", "a.pdf|same|")
	first := OrderOf("same", d, nil, false)
	for range 5 {
		if again := OrderOf("same", d, nil, false); len(again) != len(first) ||
			again[0] != first[0] {
			t.Fatalf("order changed: %v vs %v", first, again)
		}
	}
	if first[0] != 1 {
		t.Errorf("tie should break on the file name, got %v", first)
	}
}

// Negative scores are kept: a match spread through a long name is still a hit.
func TestRankingKeepsNegativeScores(t *testing.T) {
	long := strings.Repeat("x", 80) + "attention"
	d := docs(long + ".pdf|T|")
	if got := OrderOf("attention", d, nil, false); len(got) != 1 {
		t.Errorf("a long file name should still match: %v", got)
	}
}
