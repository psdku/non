// Package search ranks a library against a text query.
//
// It is separate from the TUI so the same ranking serves the interactive list
// and the non-interactive --query flag: a script and a human should see the
// same order for the same query.
package search

import (
	"sort"
	"strings"

	"github.com/sahilm/fuzzy"

	"non/internal/extract"
	"non/internal/meta"
)

// Doc is the searchable projection of one paper. The caller supplies it, so
// this package does not depend on the library type and both the TUI and the
// non-interactive query path use exactly the same ranking.
type Doc struct {
	Name string
	Meta *meta.Entry
}

// OrderOf ranks papers against the query, returning indices into papers.
//
// Matching is tiered rather than blended into a single number, because fuzzy
// subsequence matching over a long abstract produces hits that mean nothing
// ("vllm" appears in the letters of an unrelated sentence) while scoring wildly
// differently from a hit in a 20 character file name:
//
//  1. tier 0: the file name or the title matches fuzzily. This is what a user
//     expects to come first when searching for a paper by name.
//  2. tier 1: author or tags match fuzzily.
//  3. tier 2: the body fields (abstract, comment, extra metadata) contain the
//     query as a substring, which only happens when metadata search is on.
//
// Within a tier, the fuzzy score decides, then the file name for stability.
//
// docs is the already-filtered candidate set. pos maps each doc back to the
// caller's own numbering (a library position, say); pass nil for identity.
// Getting this wrong silently returns the right *count* of results with the
// wrong papers, so the mapping lives here rather than at each call site.
func OrderOf(q string, docs []Doc, pos []int, withMeta bool) []int {
	type scored struct {
		idx   int
		tier  int
		score int
	}
	var hits []scored

	consider := func(best *scored, text string, tier, weight int) {
		if text == "" {
			return
		}
		// Scores are negative for matches spread over long text; they are kept
		// as-is, because the tier already separates a name hit from a body hit
		// and discarding negatives would make queries fail on long file names.
		for _, m := range fuzzy.Find(q, []string{text}) {
			if tier < best.tier || (tier == best.tier && m.Score*weight > best.score) {
				best.tier = tier
				best.score = m.Score * weight
			}
		}
	}

	all := docs
	for i, p := range docs {
		best := scored{idx: i, tier: 4, score: 0}
		consider(&best, p.Name, 0, 3)
		consider(&best, extract.TitleFromFilename(p.Name), 0, 2)
		if p.Meta != nil {
			// The title is shown in the list, so it always participates;
			// everything below is only matched when metadata search is on.
			consider(&best, p.Meta.Title, 0, 3)
			if withMeta {
				consider(&best, p.Meta.Author, 1, 1)
				consider(&best, strings.Join(p.Meta.Tags, " "), 1, 1)
				body := []string{p.Meta.Abstract, p.Meta.Comment}
				for _, v := range p.Meta.Fields {
					body = append(body, string(v))
				}
				if best.tier > 2 && containsFold(body, q) {
					best.tier = 2
				}
			}
		}
		if best.tier <= 2 {
			hits = append(hits, best)
		}
	}
	sort.SliceStable(hits, func(a, b int) bool {
		if hits[a].tier != hits[b].tier {
			return hits[a].tier < hits[b].tier
		}
		if hits[a].score != hits[b].score {
			return hits[a].score > hits[b].score
		}
		return all[hits[a].idx].Name < all[hits[b].idx].Name
	})
	out := make([]int, 0, len(hits))
	for _, h := range hits {
		if pos != nil {
			out = append(out, pos[h.idx])
		} else {
			out = append(out, h.idx)
		}
	}
	return out
}

// containsFold reports whether any of the strings contains q, ignoring case.
func containsFold(haystacks []string, q string) bool {
	q = strings.ToLower(q)
	for _, h := range haystacks {
		if strings.Contains(strings.ToLower(h), q) {
			return true
		}
	}
	return false
}
