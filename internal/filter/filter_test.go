package filter

import (
	"strings"
	"testing"
)

// has builds a membership test from a list of tags, case-insensitively, the way
// the TUI does it.
func has(tags ...string) func(string) bool {
	set := map[string]bool{}
	for _, t := range tags {
		set[strings.ToLower(t)] = true
	}
	return func(name string) bool { return set[strings.ToLower(name)] }
}

func TestParseEval(t *testing.T) {
	cases := []struct {
		expr string
		tags []string
		want bool
	}{
		// single tags
		{"LLM", []string{"LLM"}, true},
		{"LLM", []string{"DB"}, false},
		{"llm", []string{"LLM"}, true},
		// negation, both spellings
		{"!LLM", []string{"DB"}, true},
		{"!LLM", []string{"LLM"}, false},
		{"not LLM", []string{"DB"}, true},
		{"not LLM", []string{"LLM"}, false},
		// and, both spellings
		{"LLM && DB", []string{"LLM", "DB"}, true},
		{"LLM && DB", []string{"LLM"}, false},
		{"LLM and DB", []string{"LLM", "DB"}, true},
		// or, both spellings
		{"LLM || DB", []string{"DB"}, true},
		{"LLM || DB", []string{"other"}, false},
		{"LLM or DB", []string{"DB"}, true},
		// the example from the request
		{"!LLM && DB", []string{"DB"}, true},
		{"!LLM && DB", []string{"DB", "LLM"}, false},
		// implicit AND by juxtaposition
		{"LLM DB", []string{"LLM", "DB"}, true},
		{"LLM DB", []string{"LLM"}, false},
		// parentheses change meaning
		{"(LLM || DB) && survey", []string{"DB", "survey"}, true},
		{"(LLM || DB) && survey", []string{"DB"}, false},
		{"LLM || DB && survey", []string{"LLM"}, true}, // && binds tighter
		{"LLM || DB && survey", []string{"DB"}, false},
		// ! binds tighter than &&, && tighter than ||
		{"!LLM || DB", []string{"DB", "LLM"}, true},
		{"!LLM || DB", []string{"LLM"}, false},
		{"!!LLM", []string{"LLM"}, true},
		{"!!!LLM", []string{"LLM"}, false},
		// quoted names may hold spaces and operators
		{`"KV cache"`, []string{"KV cache"}, true},
		{`"KV cache"`, []string{"KV"}, false},
		{`'KV cache'`, []string{"KV cache"}, true},
		{`"a&&b"`, []string{"a&&b"}, true},
		{`"a&&b"`, []string{"a"}, false},
		{`"say \"hi\""`, []string{`say "hi"`}, true},
		// empty query means no filter
		{"", []string{"x"}, true},
		{"   ", []string{"x"}, true},
	}
	for _, c := range cases {
		n, err := Parse(c.expr)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.expr, err)
			continue
		}
		got := true
		if n != nil {
			got = n.Eval(has(c.tags...))
		}
		if got != c.want {
			t.Errorf("%q with tags %v = %v, want %v", c.expr, c.tags, got, c.want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct{ expr, want string }{
		{"LLM &&", `"&&" needs an expression on its right`},
		{"|| LLM", `"||" needs an expression on its left`},
		{"LLM |", `stray "|"`},
		{"LLM &", `stray "&"`},
		{"!", `"!" needs an expression after it`},
		{"(LLM", `missing ")"`},
		{"LLM)", `unexpected ")"`},
		{"()", "empty parentheses"},
		{`"unterminated`, "unterminated quote"},
		{`""`, "empty tag name"},
		{"LLM && || DB", `"||" needs an expression on its left`},
	}
	for _, c := range cases {
		_, err := Parse(c.expr)
		if err == nil {
			t.Errorf("Parse(%q) succeeded, want an error", c.expr)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("Parse(%q) error = %q, want it to contain %q", c.expr, err, c.want)
		}
	}
}

func TestParseErrorPosition(t *testing.T) {
	_, err := Parse("LLM && )")
	pe, ok := err.(*ParseError)
	if !ok {
		t.Fatalf("error is %T, want *ParseError", err)
	}
	// The ")" is at byte 7.
	if pe.Pos != 7 {
		t.Errorf("Pos = %d, want 7", pe.Pos)
	}
	if !strings.Contains(pe.Error(), "position 8") {
		t.Errorf("Error() = %q, want a 1-based position", pe.Error())
	}
}

func TestStringRoundTrip(t *testing.T) {
	// Normalising an expression and re-parsing it must give the same answer,
	// so the rendered form shown to the user is always valid input.
	exprs := []string{
		"LLM && !DB",
		"(LLM || DB) && survey",
		"a b c",
		`"KV cache" && !survey`,
		"!a || (b c)",
	}
	for _, e := range exprs {
		n, err := Parse(e)
		if err != nil {
			t.Fatalf("Parse(%q): %v", e, err)
		}
		rendered := n.String()
		again, err := Parse(rendered)
		if err != nil {
			t.Fatalf("re-parsing %q (from %q): %v", rendered, e, err)
		}
		for _, tags := range [][]string{{"LLM"}, {"DB"}, {"a", "b", "c"}, {"KV cache"}, {"survey", "a"}} {
			if n.Eval(has(tags...)) != again.Eval(has(tags...)) {
				t.Errorf("%q rendered as %q changes the result for %v", e, rendered, tags)
			}
		}
	}
}

func TestTagsAndUnknown(t *testing.T) {
	n, err := Parse("LLM && (DB || llm) && !survey")
	if err != nil {
		t.Fatal(err)
	}
	if got := Tags(n); strings.Join(got, ",") != "LLM,DB,llm,survey" {
		t.Errorf("Tags = %v", got)
	}
	known := func(name string) bool {
		return strings.EqualFold(name, "LLM") || strings.EqualFold(name, "survey")
	}
	// DB is unknown, llm is a known tag in another case, and each name is
	// reported once.
	if got := UnknownTags(n, known); strings.Join(got, ",") != "DB" {
		t.Errorf("UnknownTags = %v, want [DB]", got)
	}
}

func TestUnknownTagsOnEmptyExpression(t *testing.T) {
	n, err := Parse("")
	if err != nil {
		t.Fatal(err)
	}
	if got := UnknownTags(n, func(string) bool { return false }); len(got) != 0 {
		t.Errorf("UnknownTags of empty = %v", got)
	}
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{"LLM", "!LLM && DB", `"a b"`, "(a || b) && c", "a b c", `"`, "&&", "()"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		n, err := Parse(s)
		if err != nil {
			// An error must be a ParseError with an in-range position.
			pe, ok := err.(*ParseError)
			if !ok {
				t.Fatalf("error type %T for %q", err, s)
			}
			if pe.Pos < 0 || pe.Pos > len(s) {
				t.Fatalf("Pos %d out of range for %q", pe.Pos, s)
			}
			return
		}
		if n == nil {
			return
		}
		// Evaluation and rendering must never panic, and the rendered form
		// must parse.
		_ = n.Eval(func(string) bool { return true })
		if _, err := Parse(n.String()); err != nil {
			t.Fatalf("%q rendered as %q does not parse: %v", s, n.String(), err)
		}
	})
}
