package meta

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGlobalConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	g := &GlobalConfig{}
	g.UnmarshalJSON([]byte(`{}`))
	g.path = path
	g.DefaultDir = "~/papers"
	g.Tools = []Tool{{Name: "zathura", Command: "zathura --fork", Type: ToolTUI}}
	g.TagTable = []Tag{{Name: "kvcache", Description: "KV cache"}}
	if err := g.Save(); err != nil {
		t.Fatal(err)
	}
	back, err := LoadGlobalAt(path)
	if err != nil {
		t.Fatal(err)
	}
	if back.DefaultDir != "~/papers" {
		t.Errorf("DefaultDir = %q", back.DefaultDir)
	}
	if len(back.Tools) != 1 || back.Tools[0].Name != "zathura" {
		t.Errorf("Tools = %+v", back.Tools)
	}
	if len(back.TagTable) != 1 || back.TagTable[0].Name != "kvcache" {
		t.Errorf("TagTable = %+v", back.TagTable)
	}
}

func TestGlobalConfigMissingFile(t *testing.T) {
	g, err := LoadGlobalAt(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("a missing config should not be an error: %v", err)
	}
	if g.DefaultDir != "" {
		t.Errorf("DefaultDir = %q", g.DefaultDir)
	}
}

func TestGlobalConfigPreservesUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	raw := `{"default_dir":"/x","future":{"a":1}}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := LoadGlobalAt(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Save(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "future") {
		t.Errorf("unknown key dropped:\n%s", b)
	}
}

// A collection with no tools or tags adopts the global ones; one with its own
// keeps them.
func TestStoreInherit(t *testing.T) {
	g := &GlobalConfig{
		Tools:    []Tool{{Name: "zathura", Command: "zathura", Type: ToolTUI}},
		TagTable: []Tag{{Name: "kvcache", Description: "KV cache"}},
	}

	// Empty collection: everything is adopted.
	s := New()
	s.Inherit(g)
	if len(s.Config.Tools) != 1 || s.Config.Tools[0].Name != "zathura" {
		t.Errorf("tools = %+v", s.Config.Tools)
	}
	if s.Config.DefaultTool != "zathura" {
		t.Errorf("default tool = %q, want the inherited one", s.Config.DefaultTool)
	}
	if len(s.Config.TagTable) != 1 {
		t.Errorf("tags = %+v", s.Config.TagTable)
	}

	// A collection with its own config keeps it.
	s2 := New()
	s2.Config.Tools = []Tool{{Name: "mine", Command: "mine", Type: ToolGUI}}
	s2.Config.DefaultTool = "mine"
	s2.Config.TagTable = []Tag{{Name: "local"}}
	s2.Inherit(g)
	if len(s2.Config.Tools) != 1 || s2.Config.Tools[0].Name != "mine" {
		t.Errorf("local tools were replaced: %+v", s2.Config.Tools)
	}
	if s2.Config.DefaultTool != "mine" {
		t.Errorf("local default replaced: %q", s2.Config.DefaultTool)
	}
	if len(s2.Config.TagTable) != 1 || s2.Config.TagTable[0].Name != "local" {
		t.Errorf("local tags were replaced: %+v", s2.Config.TagTable)
	}

	// Inheriting twice must not duplicate.
	s3 := New()
	s3.Inherit(g)
	s3.Inherit(g)
	if len(s3.Config.Tools) != 1 || len(s3.Config.TagTable) != 1 {
		t.Errorf("inherit is not idempotent: %d tools, %d tags",
			len(s3.Config.Tools), len(s3.Config.TagTable))
	}
}

// The inherited slices must be copies, or a local edit would reach back into
// the global config and appear in every other directory.
func TestStoreInheritCopies(t *testing.T) {
	g := &GlobalConfig{TagTable: []Tag{{Name: "shared"}}}
	s := New()
	s.Inherit(g)
	s.Config.TagTable[0].Name = "mutated"
	if g.TagTable[0].Name != "shared" {
		t.Error("the collection shares the global tag slice")
	}
}

func TestTagNameRules(t *testing.T) {
	valid := []string{"LLM", "kvcache", "kv_cache", "a1", "_x", "Étude", "日本語"}
	for _, n := range valid {
		if !ValidTagName(n) {
			t.Errorf("%q should be valid", n)
		}
	}
	invalid := []string{"", "kv-cache", "kv cache", "kv&cache", "a.b", `a"b`, "a/b", "a\nb"}
	for _, n := range invalid {
		if ValidTagName(n) {
			t.Errorf("%q should be invalid", n)
		}
	}
}

func TestSanitizeTagName(t *testing.T) {
	cases := map[string]string{
		"KV cache":  "KV_cache",
		"kv-cache":  "kv_cache",
		"  x  ":     "x",
		"a.b.c":     "a_b_c",
		"a&&b":      "ab",
		`a"b`:       "ab",
		"one  two":  "one_two",
		"":          "",
		"___":       "",
		"tag/name":  "tag_name",
		"tab\tname": "tab_name",
	}
	for in, want := range cases {
		if got := SanitizeTagName(in); got != want {
			t.Errorf("SanitizeTagName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReadTagCSV(t *testing.T) {
	in := "name,description\nkvcache,KV cache and paged attention\nLLM,large language models\n"
	tags, err := ReadTagCSV(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 2 {
		t.Fatalf("tags = %+v", tags)
	}
	if tags[0].Name != "kvcache" || tags[0].Description != "KV cache and paged attention" {
		t.Errorf("first = %+v", tags[0])
	}

	// A file with no header and one column is accepted too.
	tags, err = ReadTagCSV(strings.NewReader("alpha\nbeta\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 2 || tags[1].Name != "beta" {
		t.Errorf("tags = %+v", tags)
	}

	// Quoted descriptions with commas survive.
	tags, err = ReadTagCSV(strings.NewReader(`kvcache,"KV cache, paged"`))
	if err != nil {
		t.Fatal(err)
	}
	if tags[0].Description != "KV cache, paged" {
		t.Errorf("description = %q", tags[0].Description)
	}

	// Blank lines are skipped.
	tags, err = ReadTagCSV(strings.NewReader("\n\nalpha\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 1 {
		t.Errorf("tags = %+v", tags)
	}
}

func TestWriteAndReadTagCSV(t *testing.T) {
	var b strings.Builder
	tags := []Tag{{Name: "zebra", Description: "z"}, {Name: "alpha", Description: "a"}}
	if err := WriteTagCSV(&b, tags); err != nil {
		t.Fatal(err)
	}
	// Sorted, with a header.
	lines := strings.Split(strings.TrimSpace(b.String()), "\n")
	if lines[0] != "name,description" {
		t.Errorf("header = %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "alpha") {
		t.Errorf("not sorted: %q", lines[1])
	}
	back, err := ReadTagCSV(strings.NewReader(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 2 {
		t.Errorf("round trip = %+v", back)
	}
}

// Importing only adds missing tags: a local description is never overwritten.
func TestMergeTagsOnlyAdds(t *testing.T) {
	c := &Config{TagTable: []Tag{{Name: "kvcache", Description: "my own words"}}}
	res := c.MergeTags([]ImportedTag{
		{Name: "kvcache", Description: "shared words"},
		{Name: "LLM", Description: "large language models"},
		{Name: "KVCache", Description: "same tag, different case"},
		{Name: "&&", Description: "unusable"},
	})
	if len(res.Added) != 1 || res.Added[0] != "LLM" {
		t.Errorf("Added = %v", res.Added)
	}
	// Both spellings of the existing tag are reported as skipped.
	if len(res.Skipped) != 2 {
		t.Errorf("Skipped = %v", res.Skipped)
	}
	if len(res.Invalid) != 1 {
		t.Errorf("Invalid = %v", res.Invalid)
	}
	// The local description is untouched.
	if got := c.DescribeTag("kvcache"); got != "my own words" {
		t.Errorf("description overwritten: %q", got)
	}
	if len(c.TagTable) != 2 {
		t.Errorf("table = %+v", c.TagTable)
	}

	// A hyphen survives as an underscore, which is a *different* tag: the
	// import does not try to guess that the two were meant to be the same.
	c2 := &Config{TagTable: []Tag{{Name: "kvcache"}}}
	res3 := c2.MergeTags([]ImportedTag{{Name: "kv-cache"}})
	if len(res3.Added) != 1 || res3.Added[0] != "kv_cache" {
		t.Errorf("hyphenated import = %+v", res3)
	}

	// Importing the same file again changes nothing.
	before := len(c.TagTable)
	res2 := c.MergeTags([]ImportedTag{{Name: "LLM"}})
	if len(res2.Added) != 0 || len(c.TagTable) != before {
		t.Errorf("import is not idempotent: %+v", res2)
	}
}

func TestImportSummary(t *testing.T) {
	var r ImportResult
	if got := r.Summary(); got != "no tags to import" {
		t.Errorf("empty summary = %q", got)
	}
	r = ImportResult{Imported: 3, Added: []string{"a"}, Skipped: []string{"b"}, Invalid: []string{"c"}}
	s := r.Summary()
	for _, want := range []string{"added 1", "skipped 1 existing", "ignored 1 invalid"} {
		if !strings.Contains(s, want) {
			t.Errorf("summary %q is missing %q", s, want)
		}
	}
}

// A config loaded from an explicit path must save back to that path, never to
// the default location: falling back would overwrite the user's real config.
func TestGlobalConfigSaveKeepsLoadedPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	raw := `{"default_dir":"/x","future":{"a":1}}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := LoadGlobalAt(path)
	if err != nil {
		t.Fatal(err)
	}
	if g.Path() != path {
		t.Fatalf("Path = %q, want %q", g.Path(), path)
	}
	g.DefaultDir = "/y"
	if err := g.Save(); err != nil {
		t.Fatal(err)
	}
	// It landed in the temp dir...
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "/y") {
		t.Errorf("save did not reach the loaded path:\n%s", b)
	}
	// ...and nowhere else.
	real, err := GlobalConfigPath()
	if err != nil {
		t.Skip("no home directory")
	}
	if _, err := os.Stat(real); err == nil {
		rb, _ := os.ReadFile(real)
		if strings.Contains(string(rb), "/y") {
			t.Errorf("save leaked into the real config %s", real)
		}
	}
}

// An unloaded config cannot be saved: there is nowhere safe to put it.
func TestGlobalConfigSaveWithoutPathRefused(t *testing.T) {
	g := &GlobalConfig{}
	if err := g.Save(); err == nil {
		t.Error("saving a pathless config should fail")
	}
}
