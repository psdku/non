package extract

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
)

var (
	arxivRE      = regexp.MustCompile(`(?i)^arxiv[:\s]`)
	emailRE      = regexp.MustCompile(`[\w.+-]+@[\w-]+\.[\w.]+`)
	sectionRE    = regexp.MustCompile(`^(\d+(\.\d+)*\.?|[A-Z]\.)\s+[A-Z]`)
	permissionRE = regexp.MustCompile(`(?i)^(permission to make|provided proper attribution|abstract\b.*\bkeywords|copyright\b|©|all rights reserved|this work is licensed)`)
	affilRE      = regexp.MustCompile(`(?i)(university|universit[a-zäöü]*|institute|laborator|\blabs?\b|department|dept\.|school of|college|researcher|research|google|openai|microsoft|meta ai|deepmind|inc\.|corp\.|ltd|gmbh|berkeley|stanford|cmu\b|mit\b|eth\b|nvidia|independent)`)
	stopHeadings = []string{"abstract", "introduction", "contents", "references", "conclusion", "conclusions", "acknowledg", "acknowledgment", "bibliography", "appendix"}
	proseRE      = regexp.MustCompile(`^[A-Z][^.!?]{40,}[.!?]$`)
	// affilDigitRE matches a superscript affiliation marker after a name run
	// ("Woosuk Kwon 1,∗ Siyuan Zhuang 1,2"), which is where a comma belongs.
	affilDigitRE = regexp.MustCompile(`([A-Za-z.])\s?\d+(?:,\d+)*`)
	// affilLeadRE matches an affiliation line that starts with its number.
	affilLeadRE   = regexp.MustCompile(`^\d+\s+\S`)
	nonWordSuffix = []string{".pdf", ".dvi", ".tex", ".doc", ".docx", ".indd", ".qxd"}
)

// sanitize drops control characters and repairs the ligature glyphs that some
// TeX fonts encode into the C0 range (0x0b-0x1f).
func sanitize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\t':
			b.WriteByte(' ')
		case r < 0x20 || r == 0x7f:
			b.WriteString(ligature(r))
		case r >= 0xE000 && r <= 0xF8FF:
			// private use area: missing ToUnicode mapping, drop it
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ligature maps the C0 code points that TeX fonts use for ligatures. OT1 fonts
// put them at 0x0b-0x0f and T1 fonts at 0x1b-0x1f, with a different order.
// These are only reachable through the pure Go reader; poppler decodes the
// fonts properly and never produces them.
func ligature(r rune) string {
	switch r {
	// OT1 (Computer Modern)
	case 0x0b:
		return "ff"
	case 0x0c:
		return "fi"
	case 0x0d:
		return "fl"
	case 0x0e:
		return "ffi"
	case 0x0f:
		return "ffl"
	// T1 (Cork), used by Libertine and Latin Modern
	case 0x1b:
		return "fi"
	case 0x1c:
		return "ffi"
	case 0x1d:
		return "ff"
	case 0x1e:
		return "fl"
	case 0x1f:
		return "ffl"
	}
	return ""
}

func collapseSpaces(s string) string { return strings.Join(strings.Fields(s), " ") }

func lines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	return strings.Split(text, "\n")
}

// isJunkLine reports boilerplate that can never be a title.
func isJunkLine(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" {
		return true
	}
	switch {
	case arxivRE.MatchString(t):
		return true
	case permissionRE.MatchString(t):
		return true
	case strings.HasPrefix(t, "∗"), strings.HasPrefix(t, "†"), strings.HasPrefix(t, "‡"):
		return true
	case strings.HasPrefix(t, "//"), strings.HasPrefix(t, "http"):
		return true
	}
	if emailRE.MatchString(t) {
		return true
	}
	if strings.Trim(t, "0123456789 .,-–—") == "" {
		return true
	}
	return false
}

func isAbstractHeading(s string) bool {
	t := strings.ToLower(strings.TrimSpace(s))
	t = strings.TrimRight(t, ": .")
	return t == "abstract" || t == "summary"
}

// isStopLine reports lines that end an abstract section.
func isStopLine(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" {
		return false
	}
	low := strings.ToLower(t)
	low = strings.TrimRight(low, ":")
	if slices.Contains(stopHeadings, low) {
		return true
	}
	if low == "introduction" || strings.HasPrefix(low, "introduction ") {
		return true
	}
	if strings.HasPrefix(low, "keywords") || strings.HasPrefix(low, "key words") ||
		strings.HasPrefix(low, "ccs concepts") || strings.HasPrefix(low, "categories and subject") {
		return true
	}
	if sectionRE.MatchString(t) && len(strings.Fields(t)) < 12 {
		return true
	}
	if isJunkLine(t) {
		return true
	}
	return false
}

// cleanAbstract trims and bounds an abstract.
func cleanAbstract(s string) string {
	s = collapseSpaces(s)
	s = strings.TrimSpace(strings.TrimPrefix(s, ":"))
	if len([]rune(s)) < 40 {
		return ""
	}
	const maxRunes = 4000
	if r := []rune(s); len(r) > maxRunes {
		s = string(r[:maxRunes])
		if i := strings.LastIndex(s, ". "); i > maxRunes/2 {
			s = s[:i+1]
		}
	}
	return s
}

// joinLines concatenates wrapped lines, undoing end-of-line hyphenation.
// A hyphen is kept when the continuation is a compound ("English-" +
// "to-German"), and dropped when the word was simply broken ("How-" + "ever").
func joinLines(ls []string) string {
	var b strings.Builder
	for _, l := range ls {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if b.Len() == 0 {
			b.WriteString(l)
			continue
		}
		cur := b.String()
		if strings.HasSuffix(cur, "-") {
			if joined, ok := dehyphenate(cur, l); ok {
				b.Reset()
				b.WriteString(joined)
				continue
			}
		}
		b.WriteByte(' ')
		b.WriteString(l)
	}
	return b.String()
}

// dehyphenate joins the end of prev (which ends in '-') with next.
func dehyphenate(prev, next string) (string, bool) {
	first := next
	if before, _, ok := strings.Cut(next, " "); ok {
		first = before
	}
	first = strings.TrimRight(first, ".,;:!?)]}\"")
	if first == "" {
		return "", false
	}
	runes := []rune(first)
	if !unicode.IsLower(runes[0]) {
		return "", false
	}
	// The preceding word must end in a lowercase letter, otherwise the dash is
	// a separator rather than an intra-word hyphen.
	fields := strings.Fields(strings.TrimSuffix(prev, "-"))
	if len(fields) == 0 {
		return "", false
	}
	tail := []rune(fields[len(fields)-1])
	if !unicode.IsLower(tail[len(tail)-1]) {
		return "", false
	}
	stem := strings.TrimRight(strings.TrimSuffix(prev, "-"), " ")
	if strings.Contains(first, "-") {
		// "English-" + "to-German": a compound split over a line break.
		return stem + "-" + next, true
	}
	if strings.Trim(first, "abcdefghijklmnopqrstuvwxyz") != "" {
		return "", false
	}
	return stem + next, true
}

// abstractFromLines finds an abstract heading in flat text and collects the
// lines that follow it.
func abstractFromLines(ls []string) string {
	start := -1
	for i, l := range ls {
		if isAbstractHeading(l) {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	var out []string
	words := 0
	for _, raw := range ls[start+1:] {
		l := collapseSpaces(raw)
		if l == "" {
			if words > 25 {
				break
			}
			continue
		}
		if isStopLine(l) {
			break
		}
		out = append(out, l)
		words += len(strings.Fields(l))
		if words > 700 {
			break
		}
	}
	return cleanAbstract(joinLines(out))
}

// cleanMetaTitle rejects the placeholders authoring tools like to store.
func cleanMetaTitle(s string) string {
	t := collapseSpaces(s)
	if len([]rune(t)) < 4 {
		return ""
	}
	low := strings.ToLower(t)
	for _, junk := range []string{"untitled", "microsoft word", "document1", "no title", "without title", "title of paper"} {
		if strings.Contains(low, junk) {
			return ""
		}
	}
	for _, suf := range nonWordSuffix {
		if strings.HasSuffix(low, suf) {
			return ""
		}
	}
	return t
}

// similarTitle reports whether two titles are close variants of each other
// (token overlap, so ligature damage like "Ecient" still matches).
func similarTitle(a, b string) bool {
	ta, tb := titleTokens(a), titleTokens(b)
	if len(ta) == 0 || len(tb) == 0 {
		return false
	}
	set := make(map[string]bool, len(ta))
	for _, t := range ta {
		set[t] = true
	}
	hit := 0
	for _, t := range tb {
		if set[t] {
			hit++
		}
	}
	denom := max(len(tb), len(ta))
	return float64(hit)/float64(denom) >= 0.4
}

func titleTokens(s string) []string {
	var out []string
	for w := range strings.FieldsSeq(strings.ToLower(s)) {
		w = strings.Trim(w, ".,;:!?()[]{}\"'`-–—")
		if len(w) >= 3 {
			out = append(out, w)
		}
	}
	return out
}

func absf(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
