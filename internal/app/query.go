package app

import (
	"strings"

	"non/internal/filter"
	"non/internal/search"
)

// Query is a non-interactive selection: a tag expression, a text query, and
// whether the text may match metadata.
//
// It is the same pair of operations the TUI applies, in the same order —
// filter first, then rank — so a script that asks for `LLM && !DB`, `paged`
// gets the list a human would see, in the same order. Keeping this in one place
// is the only way to promise that, since the two paths otherwise drift.
type Query struct {
	Tags     filter.Node // nil means no tag filter
	Text     string      // empty means no text search
	WithMeta bool        // let Text match author, tags and body as well
}

// Empty reports whether the query selects everything.
func (q Query) Empty() bool {
	return q.Tags == nil && strings.TrimSpace(q.Text) == ""
}

// Select returns the papers matching the query, in display order.
//
// It returns indices into lib.Papers so callers keep access to the paper and
// its position. A nil filter keeps everything, exactly like the TUI's empty
// filter, so an unmatched query yields an empty slice rather than an error.
func (q Query) Select(lib *Library) []int {
	all := lib.Papers

	// Filter first: the tag expression is a predicate, and narrowing before
	// ranking keeps the ranking over the same candidate set the TUI shows.
	base := all
	if q.Tags != nil {
		kept := make([]*Paper, 0, len(all))
		for _, p := range all {
			if PaperHasTags(p, q.Tags) {
				kept = append(kept, p)
			}
		}
		base = kept
	}

	text := strings.TrimSpace(q.Text)
	if text == "" {
		// No ranking: keep library order, which is stable and sorted by name.
		out := make([]int, 0, len(base))
		pos := make(map[*Paper]int, len(all))
		for i, p := range all {
			pos[p] = i
		}
		for _, p := range base {
			out = append(out, pos[p])
		}
		return out
	}
	// Rank the filtered candidates, then map the ranks back to library
	// positions so the caller keeps a paper's identity.
	pos := make(map[*Paper]int, len(all))
	for i, p := range all {
		pos[p] = i
	}
	docs := make([]search.Doc, len(base))
	for i, p := range base {
		docs[i] = search.Doc{Name: p.Name, Meta: p.Meta}
	}
	positions := make([]int, len(base))
	for i, p := range base {
		positions[i] = pos[p]
	}
	// OrderOf already mapped each rank through positions, so its result is
	// ready to use; mapping again here would index the library with a library
	// index and panic (or silently return the wrong papers).
	return search.OrderOf(text, docs, positions, q.WithMeta)
}

// PaperHasTags evaluates a tag expression against one paper.
func PaperHasTags(p *Paper, node filter.Node) bool {
	if node == nil {
		return true
	}
	var tags []string
	if p.Meta != nil {
		tags = p.Meta.Tags
	}
	return node.Eval(func(name string) bool {
		for _, t := range tags {
			if strings.EqualFold(t, name) {
				return true
			}
		}
		return false
	})
}

// HasTag reports whether a paper carries a tag, ignoring case.
func HasTag(p *Paper, name string) bool {
	if p.Meta == nil {
		return false
	}
	for _, t := range p.Meta.Tags {
		if strings.EqualFold(t, name) {
			return true
		}
	}
	return false
}
