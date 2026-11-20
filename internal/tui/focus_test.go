package tui

import (
	"strings"
	"testing"

	"non/internal/meta"
)

// focusedRows counts how many form rows believe they have the cursor.
func focusedRows(m *Model) int {
	n := 0
	for _, r := range m.metaForm.rows {
		if r.input.Focused() || r.area.Focused() {
			n++
		}
	}
	return n
}

// Moving the cursor must blur the field it left. A text input keeps drawing its
// cursor until it is blurred, so the old field used to show a phantom cursor
// while the new one was focused too.
func TestMetaFormOnlyOneFieldIsFocused(t *testing.T) {
	m := inlineModel(t, "kvcache")
	m.width, m.height = 100, 30
	m = key(m, "m")

	for i := 0; i < len(m.metaForm.rows)+2; i++ {
		if got := focusedRows(m); got > 1 {
			t.Fatalf("row %d: %d rows are focused, want at most 1", i, got)
		}
		// Exactly one reverse-video bar means exactly one visible cursor.
		if got := strings.Count(m.View(), "\x1b[7m"); got > 1 {
			t.Fatalf("row %d: %d cursors drawn", i, got)
		}
		m = key(m, "down")
	}
}

// The same for moving back up, which is where a stale cursor is most visible.
func TestMetaFormFocusClearsOnUp(t *testing.T) {
	m := inlineModel(t, "kvcache")
	m.width, m.height = 100, 30
	m = key(m, "m")
	m = key(m, "down")
	m = key(m, "down")
	if got := focusedRows(m); got != 1 {
		t.Fatalf("%d focused after moving down", got)
	}
	m = key(m, "up")
	if got := focusedRows(m); got != 1 {
		t.Fatalf("%d focused after moving up", got)
	}
	// The remaining cursor must be on the row the model points at.
	for i, r := range m.metaForm.rows {
		on := r.input.Focused() || r.area.Focused()
		if on && i != m.metaForm.cur {
			t.Errorf("row %d is focused but the cursor is on %d", i, m.metaForm.cur)
		}
	}
}

// tab moves between rows too, and must blur as it goes.
func TestMetaFormFocusClearsOnTab(t *testing.T) {
	m := inlineModel(t, "kvcache")
	m.width, m.height = 100, 30
	m = key(m, "m")
	for i := 0; i < 6; i++ {
		m = key(m, "tab")
		if got := focusedRows(m); got > 1 {
			t.Fatalf("after tab %d: %d rows focused", i, got)
		}
	}
}

// The settings tool/tag forms have the same rule: the view must not focus
// widgets, because nothing then blurs them.
func TestSettingsFormOnlyOneFieldIsFocused(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m.openSettings()
	m.settings.page = pageTools
	m.refreshTools()
	m = key(m, "a") // add a tool: opens the form
	if m.settings.page != pageToolForm {
		t.Fatalf("page = %v, want the tool form", m.settings.page)
	}
	for i := 0; i < 4; i++ {
		n := 0
		for _, f := range m.settings.form {
			if f.input.Focused() {
				n++
			}
		}
		if n > 1 {
			t.Fatalf("field %d: %d inputs focused", i, n)
		}
		if got := strings.Count(m.View(), "\x1b[7m"); got > 1 {
			t.Fatalf("field %d: %d cursors drawn", i, got)
		}
		m = key(m, "tab")
	}
}

// The tag summary caps a single-line display at three names plus a "+".
func TestTagSummary(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{}, ""},
		{[]string{"a"}, "a"},
		{[]string{"a", "b"}, "a b"},
		{[]string{"a", "b", "c"}, "a b c"},
		{[]string{"a", "b", "c", "d"}, "a b c+"},
		{[]string{"a", "b", "c", "d", "e"}, "a b c+"},
	}
	for _, c := range cases {
		if got := tagSummary(c.in); got != c.want {
			t.Errorf("tagSummary(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A paper with many tags must not squeeze its own name out of the row.
func TestListRowCapsManyTags(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m.lib.Store.Set("a.pdf", &meta.Entry{
		Title: "T",
		Tags:  []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta"},
	})
	_ = m.lib.Rescan()
	m.rebuild()

	row := stripEsc(m.listRow(0, 60))
	if !strings.Contains(row, "alpha beta gamma+") {
		t.Errorf("tags are not summarized: %q", row)
	}
	for _, extra := range []string{"delta", "epsilon", "zeta"} {
		if strings.Contains(row, extra) {
			t.Errorf("%q should be summarized away: %q", extra, row)
		}
	}
	if len([]rune(row)) != 60 {
		t.Errorf("row is %d cells, want 60: %q", len([]rune(row)), row)
	}
	// Three tags are shown in full, with no "+".
	m.lib.Store.Set("a.pdf", &meta.Entry{Title: "T", Tags: []string{"alpha", "beta", "gamma"}})
	_ = m.lib.Rescan()
	m.rebuild()
	row = stripEsc(m.listRow(0, 60))
	if strings.Contains(row, "+") {
		t.Errorf("three tags should not get a +: %q", row)
	}
	if !strings.Contains(row, "alpha beta gamma") {
		t.Errorf("tags missing: %q", row)
	}
}

// The details pane keeps the full list: it is where you go to see everything.
func TestDetailsPaneShowsAllTags(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m.lib.Store.Set("a.pdf", &meta.Entry{
		Title: "T",
		Tags:  []string{"alpha", "beta", "gamma", "delta"},
	})
	_ = m.lib.Rescan()
	m.rebuild()
	m.width, m.height = 100, 30
	out := stripEsc(m.View())
	for _, tag := range []string{"alpha", "beta", "gamma", "delta"} {
		if !strings.Contains(out, "#"+tag) {
			t.Errorf("details pane is missing #%s:\n%s", tag, out)
		}
	}
}
