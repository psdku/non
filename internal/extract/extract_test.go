package extract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtures locates the sample papers in the repository's samples directory;
// tests skip when the repository was copied without them.
func fixtures(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("../../samples")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "1706.03762v7.pdf")); err != nil {
		t.Skip("sample PDFs not present")
	}
	return dir
}

// The sample papers must yield their real titles and abstracts.
func TestExtractFixtures(t *testing.T) {
	dir := fixtures(t)
	cases := []struct {
		file     string
		title    string
		author   string
		abstract string
		pages    int
	}{
		{
			file:     "1706.03762v7.pdf",
			title:    "Attention Is All You Need",
			author:   "Vaswani",
			abstract: "The dominant sequence transduction models",
			pages:    15,
		},
		{
			file:     "2005.14165v4.pdf",
			title:    "Language Models are Few-Shot Learners",
			author:   "Tom B. Brown",
			abstract: "Recent work has demonstrated substantial gains",
			pages:    75,
		},
		{
			file:     "2309.06180v1.pdf",
			title:    "Efficient Memory Management for Large Language Model Serving with PagedAttention",
			author:   "Woosuk Kwon",
			abstract: "High throughput serving of large language models",
			pages:    16,
		},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			res := Extract(filepath.Join(dir, c.file))
			if res.Title != c.title {
				t.Errorf("Title = %q, want %q", res.Title, c.title)
			}
			if !strings.Contains(res.Author, c.author) {
				t.Errorf("Author = %q, want it to contain %q", res.Author, c.author)
			}
			if !strings.HasPrefix(res.Abstract, c.abstract) {
				t.Errorf("Abstract = %q, want prefix %q", trunc(res.Abstract, 120), c.abstract)
			}
			if res.Pages != c.pages {
				t.Errorf("Pages = %d, want %d", res.Pages, c.pages)
			}
			if res.Engine == "none" {
				t.Errorf("Engine = %q", res.Engine)
			}
		})
	}
}

// The abstract must be a complete paragraph: it should end with a sentence and
// not run into the following section.
func TestAbstractIsBounded(t *testing.T) {
	dir := fixtures(t)
	res := Extract(filepath.Join(dir, "1706.03762v7.pdf"))
	if len(res.Abstract) > 2000 {
		t.Errorf("abstract is %d bytes, looks unbounded", len(res.Abstract))
	}
	if !strings.HasSuffix(res.Abstract, ".") {
		t.Errorf("abstract does not end with a period: %q", trunc(res.Abstract, 120))
	}
	for _, stop := range []string{"Equal contribution", "1 Introduction", "References"} {
		if strings.Contains(res.Abstract, stop) {
			t.Errorf("abstract ran into %q: %q", stop, trunc(res.Abstract, 200))
		}
	}
}

// Hyphenation across line breaks must be repaired without destroying real
// compounds.
func TestAbstractHyphenation(t *testing.T) {
	dir := fixtures(t)
	res := Extract(filepath.Join(dir, "1706.03762v7.pdf"))
	for _, bad := range []string{"English- to-German", "How- ever", "single- model"} {
		if strings.Contains(res.Abstract, bad) {
			t.Errorf("abstract contains broken hyphenation %q", bad)
		}
	}
	for _, good := range []string{"English-to-German", "English-to-French", "single-model", "state-of-the-art"} {
		if !strings.Contains(res.Abstract, good) {
			t.Errorf("abstract lost compound %q", good)
		}
	}
}

func TestTitleFromLines(t *testing.T) {
	// A synthetic first page: a title in the largest type, then authors and
	// body text.
	lines := []line{
		{y: 700, size: 10, text: "arXiv:1234.5678v1 [cs.CL] 1 Jan 2024", rotated: true},
		{y: 690, size: 20, text: "A Study of"},
		{y: 665, size: 20, text: "Things That Matter"},
		{y: 640, size: 10, text: "Jane Doe"},
		{y: 630, size: 10, text: "Some University"},
		{y: 610, size: 10, text: "Abstract"},
		{y: 600, size: 10, text: "We study things."},
	}
	if got := titleFromLines(lines); got != "A Study of Things That Matter" {
		t.Errorf("titleFromLines = %q", got)
	}
}

func TestTitleIgnoresBoilerplate(t *testing.T) {
	lines := []line{
		{y: 700, size: 14, text: "Provided proper attribution is provided, Google hereby grants"},
		{y: 690, size: 14, text: "permission to reproduce the tables and figures in this paper"},
		{y: 660, size: 17, text: "Real Title Here"},
		{y: 640, size: 10, text: "Author Name"},
	}
	if got := titleFromLines(lines); got != "Real Title Here" {
		t.Errorf("titleFromLines = %q", got)
	}
}

func TestTitleRejectsBodyText(t *testing.T) {
	// Uniform type sizes: there is no title, so extraction must say so rather
	// than return the first line of the body.
	var lines []line
	for i := 0; i < 10; i++ {
		lines = append(lines, line{y: float64(700 - i*12), size: 10, text: "body text line here"})
	}
	if got := titleFromLines(lines); got != "" {
		t.Errorf("titleFromLines = %q, want empty", got)
	}
}

func TestTitleSkipsRotatedLines(t *testing.T) {
	lines := []line{
		{y: 700, size: 40, text: "Memory usage (GB)", rotated: true},
		{y: 650, size: 18, text: "Vertical Axis Paper"},
		{y: 630, size: 9, text: "body"},
	}
	if got := titleFromLines(lines); got != "Vertical Axis Paper" {
		t.Errorf("titleFromLines = %q", got)
	}
}

func TestAbstractFromLines(t *testing.T) {
	text := strings.Join([]string{
		"Some Journal 2024",
		"Abstract",
		"We present a thing that does stuff. It works well and is",
		"fast, and we show it on many benchmarks with good results.",
		"1 Introduction",
		"Body text goes here and is long enough to be ignored.",
	}, "\n")
	got := abstractFromLines(lines(text))
	want := "We present a thing that does stuff. It works well and is fast, and we show it on many benchmarks with good results."
	if got != want {
		t.Errorf("abstractFromLines =\n %q\nwant\n %q", got, want)
	}
}

func TestSanitizeLigatures(t *testing.T) {
	// TeX fonts encode ligatures in the C0 range. The sample papers use T1
	// (Cork) encoding, where 0x1c is "ffi" and 0x1d is "ff".
	if got := sanitize("E\x1ccient"); got != "Efficient" {
		t.Errorf("sanitize = %q", got)
	}
	if got := sanitize("di\x1derent"); got != "different" {
		t.Errorf("sanitize = %q", got)
	}
	// OT1 encoding uses a different layout: 0x0c is "fi".
	if got := sanitize("de\x0cne"); got != "define" {
		t.Errorf("OT1 sanitize = %q", got)
	}
	if got := sanitize("a\tb"); got != "a b" {
		t.Errorf("sanitize = %q", got)
	}
	// Private use area glyphs have no Unicode mapping and are dropped.
	if got := sanitize("a\ue000b"); got != "ab" {
		t.Errorf("sanitize = %q", got)
	}
}

func TestJoinLines(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"How-", "ever, this"}, "However, this"},
		{[]string{"English-", "to-German"}, "English-to-German"},
		// A line-break hyphen between two lowercase words is treated as
		// hyphenation and removed; genuine compounds that span a break
		// ("state-" + "of-the-art") are covered above.
		{[]string{"informa-", "tion"}, "information"},
		{[]string{"plain", "join"}, "plain join"},
	}
	for _, c := range cases {
		if got := joinLines(c.in); got != c.want {
			t.Errorf("joinLines(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCleanMetaTitle(t *testing.T) {
	for _, bad := range []string{"", "untitled", "Microsoft Word - draft.doc", "paper.pdf", "Document1"} {
		if got := cleanMetaTitle(bad); got != "" {
			t.Errorf("cleanMetaTitle(%q) = %q, want empty", bad, got)
		}
	}
	if got := cleanMetaTitle("A Real Title"); got != "A Real Title" {
		t.Errorf("cleanMetaTitle = %q", got)
	}
}

func TestSimilarTitle(t *testing.T) {
	if !similarTitle("Eficient Memory Management", "Efficient Memory Management") {
		t.Error("ligature-damaged titles should match")
	}
	if similarTitle("Attention Is All You Need", "Language Models are Few-Shot Learners") {
		t.Error("unrelated titles should not match")
	}
}

func TestTitleFromFilename(t *testing.T) {
	cases := map[string]string{
		"1706.03762v7.pdf":      "1706.03762v7",
		"my_paper-final.pdf":    "my paper final",
		"/tmp/some dir/x_y.pdf": "x y",
	}
	for in, want := range cases {
		if got := TitleFromFilename(in); got != want {
			t.Errorf("TitleFromFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTitleOrFallback(t *testing.T) {
	res := Result{}
	if got := res.TitleOr("some_file.pdf"); got != "some file" {
		t.Errorf("TitleOr = %q", got)
	}
	res.Title = "Real"
	if got := res.TitleOr("some_file.pdf"); got != "Real" {
		t.Errorf("TitleOr = %q", got)
	}
}

func TestExtractMissingFile(t *testing.T) {
	res := Extract(filepath.Join(t.TempDir(), "nope.pdf"))
	if res.Engine != "none" {
		t.Errorf("Engine = %q", res.Engine)
	}
	if res.Warn == "" {
		t.Error("expected a warning for a missing file")
	}
	if got := res.TitleOr("nope.pdf"); got != "nope" {
		t.Errorf("TitleOr = %q", got)
	}
}

func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
