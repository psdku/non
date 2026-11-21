package meta

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A tag is unique regardless of case, so adding or renaming onto an existing
// spelling must be refused rather than creating a row the user cannot tell
// apart.
func TestSetTagRejectsCaseDuplicate(t *testing.T) {
	var c Config
	c.SetTag(-1, Tag{Name: "LLM"})
	c.SetTag(-1, Tag{Name: "llm"})
	c.SetTag(-1, Tag{Name: "LlM"})
	if got := c.TagNames(); len(got) != 1 || got[0] != "LLM" {
		t.Errorf("table = %v, want a single LLM", got)
	}
	// Renaming an existing row onto another is refused too.
	c.SetTag(-1, Tag{Name: "DB"})
	c.SetTag(1, Tag{Name: "LLM"})
	if got := c.TagNames(); len(got) != 2 {
		t.Errorf("rename created a duplicate: %v", got)
	}
	// A rename that only changes case is allowed, and keeps one row.
	c.SetTag(0, Tag{Name: "llm"})
	if got := c.TagNames(); len(got) != 2 || got[0] != "llm" && got[1] != "llm" {
		t.Errorf("case-only rename failed: %v", got)
	}
}

func TestEnsureTagIsCaseInsensitive(t *testing.T) {
	var c Config
	canonical, added := c.EnsureTag("LLM")
	if !added || canonical != "LLM" {
		t.Fatalf("first add: %q %v", canonical, added)
	}
	canonical, added = c.EnsureTag("llm")
	if added {
		t.Error("a different case was treated as a new tag")
	}
	if canonical != "LLM" {
		t.Errorf("canonical = %q, want the table's spelling", canonical)
	}
	if len(c.TagTable) != 1 {
		t.Errorf("table = %v", c.TagNames())
	}
}

// A file hand-edited into duplicates is repaired on load, without asking: no
// information is lost, so there is nothing to confirm.
func TestLoadNormalizesDuplicates(t *testing.T) {
	dir := t.TempDir()
	raw := `{
	  "version": 1,
	  "entries": {"a.pdf": {"title": "A", "tags": ["LLM", "llm", "LLM"]}},
	  "config": {"tags": [{"name": "LLM"}, {"name": "llm", "description": "lower row"}]}
	}`
	if err := os.WriteFile(filepath.Join(dir, StoreFile), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Entries["a.pdf"].Tags; len(got) != 1 || got[0] != "LLM" {
		t.Errorf("entry tags = %v, want [LLM]", got)
	}
	if got := s.Config.TagNames(); len(got) != 1 || got[0] != "LLM" {
		t.Errorf("table = %v, want [LLM]", got)
	}
	// The description from the dropped row is not lost.
	if got := s.Config.DescribeTag("LLM"); got != "lower row" {
		t.Errorf("description = %q", got)
	}
}

// Loading a broken file must not rewrite it: the repair is in memory until
// something actually saves.
func TestLoadDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, StoreFile)
	raw := `{"version":1,"entries":{"a.pdf":{"title":"A","tags":["x","x"]}},"config":{"tags":[{"name":"LLM"}]}}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err != nil {
		t.Fatal(err)
	}
	back, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(back) != raw {
		t.Errorf("load modified the file:\n%s", back)
	}
}

// An unknown tag is reported, never deleted.
func TestProblemsReportsUnknownTags(t *testing.T) {
	dir := t.TempDir()
	raw := `{"version":1,"entries":{"a.pdf":{"title":"A","tags":["LLM","typo"]}},
	         "config":{"tags":[{"name":"LLM"}]}}`
	if err := os.WriteFile(filepath.Join(dir, StoreFile), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	probs := s.Problems()
	if len(probs) != 1 || probs[0].Tag != "typo" || probs[0].Entry != "a.pdf" {
		t.Errorf("problems = %+v", probs)
	}
	if got := s.UnknownTags(); len(got) != 1 || got[0] != "typo" {
		t.Errorf("unknown = %v", got)
	}
	// The tag is still in the entry, and only the known one is displayed.
	if len(s.Entries["a.pdf"].Tags) != 2 {
		t.Errorf("the unknown tag was dropped from the file: %v", s.Entries["a.pdf"].Tags)
	}
	if got := s.ValidTags("a.pdf"); len(got) != 1 || got[0] != "LLM" {
		t.Errorf("valid tags = %v", got)
	}
}

func TestProblemsReportsMalformedNames(t *testing.T) {
	s := New()
	s.Config.SetTag(-1, Tag{Name: "LLM"})
	s.Set("a.pdf", &Entry{Title: "A", Tags: []string{"LLM", "bad-name"}})
	probs := s.Problems()
	if len(probs) != 1 || probs[0].Tag != "bad-name" {
		t.Errorf("problems = %+v", probs)
	}
	if got := s.ValidTags("a.pdf"); len(got) != 1 {
		t.Errorf("valid = %v", got)
	}
}

// Unknown tags are reported per entry, lowercased, so a display can mark them.
func TestProblemTagsFor(t *testing.T) {
	s := New()
	s.Config.SetTag(-1, Tag{Name: "LLM"})
	s.Set("a.pdf", &Entry{Title: "A", Tags: []string{"LLM", "Typo", "typo"}})
	got := s.ProblemTagsFor("a.pdf")
	if len(got) != 1 || !got["typo"] {
		t.Errorf("problem tags = %v", got)
	}
	if got := s.ProblemTagsFor("missing.pdf"); got != nil {
		t.Errorf("unknown entry = %v", got)
	}
}

func TestValidateTags(t *testing.T) {
	s := New()
	s.Config.SetTag(-1, Tag{Name: "LLM"})
	if bad := s.ValidateTags([]string{"LLM", "llm"}); len(bad) != 0 {
		t.Errorf("valid tags rejected: %v", bad)
	}
	bad := s.ValidateTags([]string{"LLM", "typo", "bad-name"})
	if len(bad) != 2 {
		t.Errorf("bad = %v", bad)
	}
}

// Problems are reported in a stable order so a message does not shuffle.
func TestProblemsAreSorted(t *testing.T) {
	s := New()
	s.Set("b.pdf", &Entry{Title: "B", Tags: []string{"z"}})
	s.Set("a.pdf", &Entry{Title: "A", Tags: []string{"y"}})
	probs := s.Problems()
	if len(probs) != 2 {
		t.Fatalf("problems = %+v", probs)
	}
	if probs[0].Entry != "a.pdf" || probs[1].Entry != "b.pdf" {
		t.Errorf("not sorted by entry: %+v", probs)
	}
	// UnknownTags is sorted and distinct.
	s.Set("c.pdf", &Entry{Title: "C", Tags: []string{"y", "x"}})
	got := s.UnknownTags()
	want := []string{"x", "y", "z"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("unknown = %v, want %v", got, want)
	}
}

// A save canonicalises: a caller-built entry gets one spelling per tag.
func TestSaveCanonicalizes(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Config.SetTag(-1, Tag{Name: "LLM"})
	s.Set("a.pdf", &Entry{Title: "A", Tags: []string{"llm", "LLM", "llm"}})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	back, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := back.Entries["a.pdf"].Tags; len(got) != 1 || got[0] != "LLM" {
		t.Errorf("saved tags = %v, want [LLM]", got)
	}
}
