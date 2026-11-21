package meta

import (
	"os"
	"path/filepath"
	"testing"
)

// The allowed set is what people actually name topics with.
func TestTagNameAllowed(t *testing.T) {
	valid := []string{
		"LLM", "kv_cache", "attention-mechanisms", "R&D",
		"a-b-c", "x&y", "  padded  ", "日本語",
	}
	for _, n := range valid {
		if !ValidTagName(n) {
			t.Errorf("%q should be valid", n)
		}
	}
	invalid := []string{
		"", "   ", `a"b`, `a'b`, `a\\b`, "a&&b", "a||b", "a|b", "a!b", "a,b", "a:b",
		"retrieval-(dense)", "(", ")", "&", "---", "()",
	}
	for _, n := range invalid {
		if ValidTagName(n) {
			t.Errorf("%q should be invalid", n)
		}
	}
}

// A reserved pair is refused, but a lone operator character is not: "R&D" must
// work while "R&&D" cannot, or a filter could not tell them apart.
func TestTagNameReservedPairs(t *testing.T) {
	if ValidTagName("R&&D") {
		t.Error("&& should be reserved")
	}
	if ValidTagName("a||b") {
		t.Error("|| should be reserved")
	}
	if !ValidTagName("R&D") {
		t.Error("a lone & should be allowed")
	}
}

func TestSanitizeTagNameKeepsAllowedCharacters(t *testing.T) {
	cases := map[string]string{
		// A space becomes a dash: two adjacent names mean AND in a filter, so a
		// space in a name would force it to be quoted in every query.
		"KV cache":          "KV-cache",
		"kv-cache":          "kv-cache",
		"R&D":               "R&D",
		"retrieval (dense)": "retrieval-dense",
		"a(b)c":             "a-b-c",
		"(x)":               "x",
		"a.b":               "a_b",
		"a/b":               "a_b",
		`a"b`:               "a_b",
		"  x  ":             "x",
		"":                  "",
		"___":               "",
		"a  b":              "a-b",
		"-lead":             "lead",
		"trail-":            "trail",
	}
	for in, want := range cases {
		if got := SanitizeTagName(in); got != want {
			t.Errorf("SanitizeTagName(%q) = %q, want %q", in, got, want)
		}
	}
}

// Whatever the sanitizer produces must be usable, or an import would add a name
// that is immediately reported as a problem.
func TestSanitizeTagNameAlwaysValid(t *testing.T) {
	for _, in := range []string{
		"a&&b", "a||b", `a"b'c`, "a\\\\b", "a|b", "a!b", "()", "&", "a&b",
		"x (y) [z]", "\t\t", "a&&&b", "&&a&&", "日本 語",
	} {
		out := SanitizeTagName(in)
		if out == "" {
			continue // nothing usable, which callers report as invalid
		}
		if !ValidTagName(out) {
			t.Errorf("SanitizeTagName(%q) = %q, which is not valid", in, out)
		}
	}
}

// An old store may hold a name the current rule refuses. It must still load.
func TestInvalidNameStillLoads(t *testing.T) {
	s := New()
	s.Config.SetTag(-1, Tag{Name: "LLM"})
	s.Set("a.pdf", &Entry{Title: "A", Tags: []string{"LLM", "bad|name"}})
	// Normalize must not drop it: the file is the user's, and the problem is
	// reported, not silently repaired.
	s.Normalize()
	if got := s.Entries["a.pdf"].Tags; len(got) != 2 {
		t.Errorf("tags = %v", got)
	}
	probs := s.Problems()
	if len(probs) != 1 || probs[0].Tag != "bad|name" {
		t.Errorf("problems = %+v", probs)
	}
}

// A name that breaks a query is critical; one that merely looks untidy is not.
func TestCriticalTagName(t *testing.T) {
	critical := []string{"KV cache", "a|b", "a||b", "a&&b", `a"b`, `a'b`, `a\\b`, "a!b",
		"retrieval-(dense)", "(x)"}
	for _, n := range critical {
		if !CriticalTagName(n) {
			t.Errorf("%q should be critical", n)
		}
	}
	tolerated := []string{"weird.name", "a/b", "a:b", "R&D", "a_b", "a-b", "LLM"}
	for _, n := range tolerated {
		if CriticalTagName(n) {
			t.Errorf("%q should not be critical", n)
		}
	}
}

// Quoting is the graceful path: a critical name can still be queried.
func TestQuoteTagName(t *testing.T) {
	cases := map[string]string{
		"LLM":     "LLM",
		"weird.x": "weird.x",
		// A lone ampersand is a name character, so it needs no quoting; only
		// characters that would lex as something else are quoted.
		"R&D":      "R&D",
		"a&b":      "a&b",
		"KV cache": `"KV cache"`,
		"a|b":      `"a|b"`,
		`a"b`:      `"a\"b"`,
		"":         `""`,
	}
	for in, want := range cases {
		if got := QuoteTagName(in); got != want {
			t.Errorf("QuoteTagName(%q) = %q, want %q", in, got, want)
		}
	}
}

// A name the table knows must survive a load even when the current rule would
// refuse to create it: the rule governs writing, not what may exist.
func TestKnownUntidyNameSurvivesLoad(t *testing.T) {
	dir := t.TempDir()
	raw := `{"version":1,"entries":{"a.pdf":{"title":"A","tags":["weird.name"]}},
	         "config":{"tags":[{"name":"weird.name"}]}}`
	if err := os.WriteFile(filepath.Join(dir, StoreFile), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Config.Tag("weird.name"); !ok {
		t.Error("the tag row was dropped on load")
	}
	if got := len(s.Entries["a.pdf"].Tags); got != 1 {
		t.Errorf("the tag was dropped from the entry: %v", s.Entries["a.pdf"].Tags)
	}
	// It is reported but not blocking, so saving it is allowed.
	if got := s.ValidateTags([]string{"weird.name"}); len(got) != 0 {
		t.Errorf("a known untidy name was refused: %v", got)
	}
	if got := s.BlockingTags(); len(got) != 0 {
		t.Errorf("a known untidy name blocks: %v", got)
	}
	// Problems still reports it, for --check.
	probs := s.Problems()
	if len(probs) != 1 || probs[0].Critical {
		t.Errorf("problems = %+v", probs)
	}
}

// A critical name is blocking even when the table knows it, because a query
// would have to quote it.
func TestKnownCriticalNameBlocks(t *testing.T) {
	s := New()
	s.Config.TagTable = append(s.Config.TagTable, Tag{Name: "KV cache"})
	s.Set("a.pdf", &Entry{Title: "A", Tags: []string{"KV cache"}})
	if got := s.ValidateTags([]string{"KV cache"}); len(got) != 1 {
		t.Errorf("a critical name was accepted: %v", got)
	}
	if got := s.BlockingTags(); len(got) != 1 || got[0] != "KV cache" {
		t.Errorf("blocking = %v", got)
	}
}
