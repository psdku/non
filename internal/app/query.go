package app

import (
	"strings"

	"non/internal/filter"
	"non/internal/meta"
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
			if PaperHasTags(lib, p, q.Tags) {
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

// HasTags evaluates a tag expression against one paper's tag list.
func HasTags(tags []string, node filter.Node) bool {
	if node == nil {
		return true
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

// MatchTags is the tag list a query is evaluated against: everything the entry
// carries, including tags whose name would need quoting and tags the table does
// not know.
//
// That is deliberately wider than DisplayTags. Hiding an unusable tag from the
// display is a way of saying "fix this"; hiding it from the query language as
// well would leave the user unable to find the paper at all until they had
// repaired the file, and quoting a name is an explicit act -- nobody types
// "KV cache" by accident.
func (l *Library) MatchTags(p *Paper) []string {
	if p == nil || p.Meta == nil {
		return nil
	}
	return append([]string(nil), p.Meta.Tags...)
}

// PaperHasTags evaluates a tag expression against the tags a paper carries.
func PaperHasTags(lib *Library, p *Paper, node filter.Node) bool {
	return HasTags(lib.MatchTags(p), node)
}

// DisplayTags is the tag list a paper shows: the ones the tag table knows, in
// its spelling.
//
// A tag the table does not know is excluded, because it is a problem to fix
// rather than a property of the paper. A known tag is always shown, even when its
// name is untidy: a rule is not a reason to hide the user's own data.
func (l *Library) DisplayTags(p *Paper) []string {
	return validTagList(p, func(name string) bool {
		_, ok := l.Store.Config.Tag(name)
		return ok
	})
}

// validTagList filters a paper's tags through known, deduplicated.
func validTagList(p *Paper, known func(string) bool) []string {
	if p == nil || p.Meta == nil {
		return nil
	}
	out := make([]string, 0, len(p.Meta.Tags))
	for _, t := range p.Meta.Tags {
		// The gate is "is it usable", not "was it written under the current
		// rule": a known name with an untidy character can still be displayed
		// and matched, so it is kept.
		if !known(t) || meta.CriticalTagName(t) {
			continue
		}
		out = append(out, t)
	}
	return out
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
