package extract

import (
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/ledongthuc/pdf"
)

// glyph is one positioned piece of text from the pure Go reader.
type glyph struct {
	x, y, w, size float64
	s             string
}

// line is a visual line of text. y increases upwards.
type line struct {
	x, y, size float64
	text       string
	segments   []segment
	rotated    bool // text runs vertically (axis labels etc.)
}

// segment is a run of words separated from its neighbours by a wide gap, so
// that two columns or a caption are not glued into one sentence.
type segment struct {
	x    float64
	text string
}

type layoutDoc struct {
	metaTitle  string
	metaAuthor string
	pages      int
	pageW      float64
	lines      []line
}

// readLayout is the pure Go fallback engine.
func readLayout(path string) (*layoutDoc, error) {
	doc := &layoutDoc{}
	f, r, err := pdf.Open(path)
	if err != nil {
		return nil, errf("open %s: %v", filepath.Base(path), err)
	}
	defer f.Close()

	doc.pages = r.NumPage()
	if info := r.Trailer().Key("Info"); !info.IsNull() {
		doc.metaTitle = info.Key("Title").Text()
		doc.metaAuthor = info.Key("Author").Text()
	}
	if doc.pages < 1 {
		return nil, errf("%s: no pages", filepath.Base(path))
	}
	page := pageOne(r)
	if len(page.lines) == 0 {
		return nil, errf("%s: page 1 has no extractable text", filepath.Base(path))
	}
	doc.lines = page.lines
	doc.pageW = page.pageW
	return doc, nil
}

// pageOne extracts glyphs from page one and rebuilds lines. A panic inside the
// library (malformed PDFs do this) must not kill the TUI, so it is contained.
func pageOne(r *pdf.Reader) (lp layoutDoc) {
	defer func() {
		if recover() != nil {
			lp = layoutDoc{}
		}
	}()
	p := r.Page(1)
	if p.V.IsNull() {
		return lp
	}
	if box := p.V.Key("MediaBox"); box.Len() >= 4 {
		lp.pageW = box.Index(2).Float64()
	}
	var gs []glyph
	for _, t := range p.Content().Text {
		s := sanitize(t.S)
		if strings.TrimSpace(s) == "" {
			continue
		}
		size := t.FontSize
		if size <= 0 {
			size = 10
		}
		gs = append(gs, glyph{x: t.X, y: t.Y, w: t.W, size: size, s: s})
	}
	lp.lines = buildLines(gs)
	return lp
}

// buildLines groups glyphs into visual lines, top to bottom.
func buildLines(gs []glyph) []line {
	if len(gs) == 0 {
		return nil
	}
	sort.SliceStable(gs, func(i, j int) bool { return gs[i].y > gs[j].y })

	var out []line
	var cur []glyph
	flush := func() {
		if len(cur) > 0 {
			out = append(out, makeLine(cur))
			cur = nil
		}
	}
	for _, g := range gs {
		if len(cur) > 0 {
			prev := cur[len(cur)-1]
			if absf(prev.y-g.y) > 0.35*maxf(prev.size, 6) {
				flush()
			}
		}
		cur = append(cur, g)
	}
	flush()
	return out
}

func makeLine(gs []glyph) line {
	sort.SliceStable(gs, func(i, j int) bool { return gs[i].x < gs[j].x })

	l := line{x: gs[0].x, y: gs[0].y}
	sizes := map[float64]int{}
	var b, seg strings.Builder
	segX := gs[0].x
	flushSeg := func() {
		if t := strings.TrimSpace(seg.String()); t != "" {
			l.segments = append(l.segments, segment{x: segX, text: t})
		}
		seg.Reset()
	}
	for i, g := range gs {
		if i > 0 {
			prev := gs[i-1]
			gap := g.x - (prev.x + prev.w)
			switch {
			case gap > 2.2*maxf(g.size, 6):
				flushSeg()
				segX = g.x
				b.WriteByte(' ')
			case gap > 0.14*maxf(g.size, 6):
				b.WriteByte(' ')
			}
		}
		b.WriteString(g.s)
		seg.WriteString(g.s)
		sizes[g.size]++
	}
	flushSeg()
	l.text = collapseSpaces(b.String())
	l.size = dominantSize(sizes)
	return l
}

func dominantSize(counts map[float64]int) float64 {
	best, bestN := 0.0, -1
	for s, n := range counts {
		if n > bestN || (n == bestN && s > best) {
			best, bestN = s, n
		}
	}
	return best
}

// title and abstract mirror the poppler engine, so they work on lines too.
func (d *layoutDoc) title() string {
	return titleFromLines(d.lines)
}

func (d *layoutDoc) abstract() string {
	start := -1
	for i, l := range d.lines {
		if isAbstractHeading(l.text) {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	colX := d.lines[start].x
	pageW := d.pageW
	if pageW <= 0 {
		pageW = 612
	}
	var out []string
	for _, l := range d.lines[start+1:] {
		if l.rotated {
			continue
		}
		if isStopLine(l.text) || (l.size > 0 && l.size > 1.6*d.lines[start].size) {
			break
		}
		var keep []string
		for _, s := range l.segments {
			if s.x <= colX+0.22*pageW {
				keep = append(keep, s.text)
			}
		}
		if len(keep) == 0 {
			continue
		}
		out = append(out, joinSegments(keep))
		if len(out) > 45 {
			break
		}
	}
	return cleanAbstract(joinLines(out))
}

// titleFromLines implements the shared title heuristic.
func titleFromLines(lines []line) string {
	if len(lines) == 0 {
		return ""
	}
	var maxSize float64
	for _, l := range lines {
		if isJunkLine(l.text) || l.rotated {
			continue
		}
		if l.size > maxSize {
			maxSize = l.size
		}
	}
	if maxSize <= 0 {
		return ""
	}
	body := medianSize(lines)
	if maxSize < 1.15*body && maxSize < 17 {
		return ""
	}
	var run []line
	seen := false
	for _, l := range lines {
		big := l.size >= 0.88*maxSize
		if big && !isJunkLine(l.text) && !l.rotated {
			if seen && len(run) > 0 {
				prev := run[len(run)-1]
				if prev.y-l.y > 2.2*maxf(l.size, 6) {
					seen = false
					break
				}
			}
			run = append(run, l)
			seen = true
			continue
		}
		if seen {
			break
		}
	}
	if len(run) == 0 {
		return ""
	}
	var parts []string
	for _, l := range run {
		parts = append(parts, l.text)
	}
	text := strings.Trim(strings.TrimSpace(strings.Join(parts, " ")), " -–—|:")
	if len([]rune(text)) < 4 || isJunkLine(text) || isStopLine(text) {
		return ""
	}
	return text
}

// medianSize returns the median line height, a proxy for the body text size.
func medianSize(lines []line) float64 {
	sizes := make([]float64, 0, len(lines))
	for _, l := range lines {
		if l.size > 0 && !isJunkLine(l.text) {
			sizes = append(sizes, l.size)
		}
	}
	if len(sizes) == 0 {
		return 1
	}
	sort.Float64s(sizes)
	return sizes[len(sizes)/2]
}

// pagesFromPDF returns the page count using the pure Go reader (0 on error).
func pagesFromPDF(path string) (n int) {
	defer func() {
		if recover() != nil {
			n = 0
		}
	}()
	f, r, err := pdf.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	return r.NumPage()
}

// authors returns the author block that sits between the title and the
// abstract, e.g. "Ashish Vaswani, Noam Shazeer, Niki Parmar". Affiliations,
// emails and footnote markers are skipped. The result is a best effort: PDFs
// typeset the block in many different ways, so an empty string is common.
func authorsFromLines(lines []line) string {
	titleEnd := -1
	maxSize := 0.0
	for _, l := range lines {
		if !isJunkLine(l.text) && !l.rotated && l.size > maxSize {
			maxSize = l.size
		}
	}
	for i, l := range lines {
		if isAbstractHeading(l.text) {
			break
		}
		if !isJunkLine(l.text) && !l.rotated && l.size >= 0.88*maxSize {
			titleEnd = i
		}
	}
	if titleEnd < 0 || maxSize <= 0 {
		return ""
	}
	var names []string
	for _, l := range lines[titleEnd+1:] {
		if isAbstractHeading(l.text) {
			break
		}
		t := strings.TrimSpace(l.text)
		if t == "" {
			continue
		}
		if proseRE.MatchString(t) {
			break // the author block has ended and the body began
		}
		if l.rotated || strings.Contains(t, "@") {
			continue
		}
		if looksLikeAffiliation(t) || strings.HasPrefix(t, "arXiv") {
			// Names, affiliations and emails are often interleaved by column,
			// so affiliations are skipped rather than treated as an ending.
			continue
		}
		if isJunkLine(t) {
			continue
		}
		if s := cleanAuthorRun(t); s != "" {
			names = append(names, s)
		}
		if len(names) >= 40 {
			break
		}
	}
	return strings.Join(dedupe(names), ", ")
}

// looksLikeAffiliation reports lines such as "1 UC Berkeley" or "Google Brain".
func looksLikeAffiliation(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" {
		return true
	}
	if affilLeadRE.MatchString(t) {
		return true
	}
	if affilRE.MatchString(t) {
		return true
	}
	// Pure numbering or superscript markers.
	if strings.Trim(t, "0123456789 ,∗†‡*") == "" {
		return true
	}
	// A line that is mostly digits (author contribution markers).
	digits, letters := 0, 0
	for _, r := range t {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case unicode.IsLetter(r):
			letters++
		}
	}
	return letters > 0 && digits*3 > letters
}

// cleanAuthorRun strips markers and rejects text that is clearly not a name.
func cleanAuthorRun(s string) string {
	t := strings.NewReplacer("∗", "", "†", "", "‡", "", "*", "", "⊗", "").Replace(s)
	// A superscript affiliation marker separates names: "Woosuk Kwon 1,∗
	// Zhuohan Li 1,2" becomes "Woosuk Kwon, Zhuohan Li".
	t = affilDigitRE.ReplaceAllString(t, "$1, ")
	t = strings.ReplaceAll(t, " ,", ",")
	t = strings.ReplaceAll(t, ",,", ",")
	t = collapseSpaces(t)
	t = strings.Trim(t, " ,")
	t = strings.Trim(t, " ,;:")
	if t == "" {
		return ""
	}
	// Author runs are comma or space separated names: no lowercase-only words
	// longer than three letters, no sentence punctuation.
	if strings.HasSuffix(t, ".") && len(strings.Fields(t)) > 3 {
		return ""
	}
	if len(strings.Fields(t)) > 20 {
		return ""
	}
	for w := range strings.FieldsSeq(t) {
		w = strings.Trim(w, ",.;:()")
		if len([]rune(w)) > 3 && w == strings.ToLower(w) {
			return "" // "and", fine; "university", not a name
		}
	}
	return t
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := in[:0]
	for _, s := range in {
		k := strings.ToLower(s)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, s)
	}
	return out
}

// authors returns the author block found by the shared heuristic.
func (d *layoutDoc) authors() string { return authorsFromLines(d.lines) }
