package tui

import (
	"fmt"
	"strings"
	"testing"

	"non/internal/app"
	"non/internal/filter"
	"non/internal/meta"
)

// pickerModel is a library with n tags in the table and none on the paper.
func pickerModel(t *testing.T, n int) *Model {
	t.Helper()
	m := newTestModel(t, "a.pdf")
	for i := range n {
		m.lib.Store.Config.SetTag(-1, meta.Tag{Name: fmt.Sprintf("tag%02d", i)})
	}
	_ = m.lib.Rescan()
	m.rebuild()
	m.ready = true
	m.width, m.height = 100, 40
	return m
}

// openPicker puts the form on its tag row.
func openPicker(t *testing.T, m *Model) *Model {
	t.Helper()
	m = key(m, "m")
	m.metaForm.cur = rowIndex(m, rowTags)
	m.focusMetaRow()
	return m
}

// A large table must not grow the form: the picker is a bounded window.
func TestTagPickerIsBounded(t *testing.T) {
	few := openPicker(t, pickerModel(t, 3))
	many := openPicker(t, pickerModel(t, 300))

	// A small table may be a smaller box, but the cost of a huge one is capped
	// at the picker window: 300 tags and 30 tags must be the same height.
	hFew := few.metaFormHeight()
	hMany := many.metaFormHeight()
	hMid := openPicker(t, pickerModel(t, 30)).metaFormHeight()
	if hMany != hMid {
		t.Errorf("300 tags make the form %d rows, 30 tags make it %d: the picker is unbounded",
			hMany, hMid)
	}
	if limit := hFew + few.tagPickerRows() + 4; hMany > limit {
		t.Errorf("300 tags make the form %d rows, %d tags make it %d", hMany, 3, hFew)
	}
	// And the view fits the terminal.
	if got := len(strings.Split(stripEsc(many.View()), "\n")); got > many.height {
		t.Errorf("the view is %d rows on a %d row terminal", got, many.height)
	}
}

// With more tags than fit, the window scrolls to follow the cursor.
func TestTagPickerScrollsWithCursor(t *testing.T) {
	m := openPicker(t, pickerModel(t, 60))
	f := m.metaForm
	rows := m.tagPickerRows()
	if len(f.tagView) != 60 {
		t.Fatalf("view has %d tags", len(f.tagView))
	}
	if f.tagOff != 0 {
		t.Errorf("off = %d at the top", f.tagOff)
	}
	// Walk past the end of the first window.
	for i := 0; i < rows+2; i++ {
		m = key(m, "j")
	}
	if f.tagOff == 0 {
		t.Error("the window did not scroll")
	}
	// The cursor must be on screen, and the last tag shown.
	if cur := f.tagCursorView(); cur < f.tagOff || cur >= f.tagOff+rows {
		t.Errorf("cursor %d is outside the window %d+%d", cur, f.tagOff, rows)
	}
	out := stripEsc(m.View())
	if !strings.Contains(out, fmt.Sprintf("tag%02d", rows+2)) {
		t.Errorf("the cursor's tag is not rendered:\n%s", out)
	}
	// And it keeps following all the way to the end.
	for range 60 {
		m = key(m, "j")
	}
	if cur := f.tagCursorView(); cur < f.tagOff || cur >= f.tagOff+rows {
		t.Errorf("at the end, cursor %d is outside %d+%d", cur, f.tagOff, rows)
	}
}

// "/" filters, a space is part of the query rather than a toggle, enter applies
// and keeps the filter.
func TestTagPickerFilter(t *testing.T) {
	m := openPicker(t, pickerModel(t, 30))
	m = key(m, "/")
	if !m.metaForm.tagTyping {
		t.Fatal("/ did not start filtering")
	}
	for _, r := range "tag1" {
		m = key(m, string(r))
	}
	// 30 tags, ten match "tag1".
	if got := len(m.metaForm.tagView); got != 10 {
		t.Errorf("filter matched %d tags, want 10", got)
	}
	m = key(m, "enter")
	if m.metaForm.tagTyping {
		t.Error("enter did not finish filtering")
	}
	// Enter keeps the query, like the search and filter boxes on the browser.
	if got := len(m.metaForm.tagView); got != 10 {
		t.Errorf("after enter, %d tags visible, want the filter kept", got)
	}
	if m.metaForm.tagQuery != "tag1" {
		t.Errorf("query = %q, want it kept", m.metaForm.tagQuery)
	}
	// Esc clears it, so everything is reachable again.
	m = key(m, "/")
	m = key(m, "esc")
	if got := len(m.metaForm.tagView); got != 30 {
		t.Errorf("after esc, %d tags visible", got)
	}
	if m.metaForm.tagQuery != "" {
		t.Errorf("query = %q, want it cleared", m.metaForm.tagQuery)
	}
}

// A space typed into the query must reach the query, not toggle a tag. Names
// cannot contain one, but the query box must still accept a space so a partly
// typed name does not toggle something by accident.
func TestTagPickerSpaceBelongsToQuery(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m.lib.Store.Config.SetTag(-1, meta.Tag{Name: "KV-cache"})
	m.lib.Store.Config.SetTag(-1, meta.Tag{Name: "serving"})
	_ = m.lib.Rescan()
	m.rebuild()
	m.ready = true
	m.width, m.height = 100, 40
	m = openPicker(t, m)

	m = key(m, "/")
	for _, r := range "KV c" {
		m = key(m, string(r))
	}
	if got := m.metaForm.tagQuery; got != "KV c" {
		t.Errorf("query = %q, want %q", got, "KV c")
	}
	// A partial query naturally matches nothing; the space is still in the box.
	if got := len(m.metaForm.tagView); got != 0 {
		t.Errorf("a partial query matched %d tags", got)
	}
	m = key(m, "backspace")
	// Nothing was toggled by the space.
	for i, on := range m.metaForm.tagOn {
		if on {
			t.Errorf("%q was toggled while typing", m.metaForm.tags[i].Name)
		}
	}
	if got := len(m.metaForm.tagView); got != 1 {
		t.Errorf("filter matched %d, want 1", got)
	}
}

// esc leaves the filter line without closing the form, and clears it.
func TestTagPickerEscLeavesFilterOnly(t *testing.T) {
	m := openPicker(t, pickerModel(t, 10))
	m = key(m, "/")
	m = key(m, "t")
	m = key(m, "esc")
	if m.mode != modeMetaForm {
		t.Fatal("esc closed the whole form")
	}
	if m.metaForm.tagTyping {
		t.Error("still typing after esc")
	}
	if m.metaForm.tagQuery != "" {
		t.Errorf("esc kept the query %q", m.metaForm.tagQuery)
	}
	// Movement works again.
	before := m.metaForm.tagCursorView()
	m = key(m, "j")
	if m.metaForm.tagCursorView() == before && len(m.metaForm.tagView) > 1 {
		t.Error("j did not move after leaving the filter")
	}
}

// Down from the filter line enters the list, so the query can be typed and then
// a tag chosen without touching the mouse.
func TestTagPickerFilterToSelection(t *testing.T) {
	m := openPicker(t, pickerModel(t, 20))
	m = key(m, "/")
	m = key(m, "t")
	m = key(m, "down")
	if m.metaForm.tagTyping {
		t.Error("down did not leave the filter")
	}
	m = key(m, " ")
	on := 0
	for _, v := range m.metaForm.tagOn {
		if v {
			on++
		}
	}
	if on != 1 {
		t.Errorf("%d tags selected, want 1", on)
	}
}

// An old, invalid name stays selectable and removable: the rule is enforced on
// write, so a stricter rule must not lock anyone out of their own file.
func TestTagPickerShowsInvalidNames(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m.lib.Store.Config.SetTag(-1, meta.Tag{Name: "LLM"})
	m.lib.Store.Set("a.pdf", &meta.Entry{Title: "A", Tags: []string{"LLM", "bad|name"}})
	_ = m.lib.Rescan()
	m.rebuild()
	m.ready = true
	m.width, m.height = 100, 40
	m = openPicker(t, m)

	f := m.metaForm
	found := -1
	for i, tt := range f.tags {
		if tt.Name == "bad|name" {
			found = i
		}
	}
	if found < 0 {
		t.Fatal("an invalid name from an old file is not offered")
	}
	if !f.tagBad[found] {
		t.Error("it is not marked as a problem")
	}
	// Ticking it off must be possible, so saving can resolve it.
	f.rows[f.cur].tagPos = 0
	for vi, ti := range f.tagView {
		if ti == found {
			f.rows[f.cur].tagPos = vi
		}
	}
	key(m, " ")
	if f.tagOn[found] {
		t.Error("it could not be unticked")
	}
}

// While filtering, an unmatched query says so rather than showing an empty box.
func TestTagPickerNoMatch(t *testing.T) {
	m := openPicker(t, pickerModel(t, 5))
	m = key(m, "/")
	for _, r := range "zzz" {
		m = key(m, string(r))
	}
	out := stripEsc(m.View())
	if !strings.Contains(out, "no tag matches") {
		t.Errorf("no message for an empty result:\n%s", out)
	}
}

// The picker says how many tags there are, so a filtered view is not mistaken
// for the whole table.
func TestTagPickerShowsCount(t *testing.T) {
	m := openPicker(t, pickerModel(t, 42))
	out := stripEsc(m.View())
	if !strings.Contains(out, "42 tags") {
		t.Errorf("the picker does not state the size:\n%s", out)
	}
	if !strings.Contains(out, "more") {
		t.Errorf("the picker does not say more tags are hidden:\n%s", out)
	}
}

// The picker marks only the names that cannot be queried; a known untidy name is
// an ordinary tag, so a punctuation mark does not turn the form into an error.
func TestTagPickerMarksOnlyCriticalNames(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m.lib.Store.Config.SetTag(-1, meta.Tag{Name: "LLM"})
	// A row written by hand: SetTag rightly refuses the name, but the file may
	// already contain it, and that is the case under test.
	m.lib.Store.Config.TagTable = append(m.lib.Store.Config.TagTable,
		meta.Tag{Name: "weird.name"})
	m.lib.Store.Set("a.pdf", &meta.Entry{
		Title: "A",
		Tags:  []string{"weird.name", "LLM", "KV cache"},
	})
	_ = m.lib.Rescan()
	m.rebuild()
	m.ready = true
	m.width, m.height = 100, 40
	m = openPicker(t, m)

	f := m.metaForm
	for i, tt := range f.tags {
		bad := i < len(f.tagBad) && f.tagBad[i]
		switch tt.Name {
		case "KV cache":
			if !bad {
				t.Error("a name that cannot be queried is not marked")
			}
		case "weird.name", "LLM":
			if bad {
				t.Errorf("%q was marked as a problem", tt.Name)
			}
		}
	}
	// Saving is allowed: the only unusable tag can be unticked, and the rest are
	// written as they are.
	for i, tt := range f.tags {
		if tt.Name == "KV cache" {
			f.tagOn[i] = false
		}
	}
	m = key(m, "ctrl+s")
	if m.mode != modeBrowse {
		t.Fatalf("saving a tolerated name was refused: %q", m.metaForm.status)
	}
	e, _ := m.lib.Store.Lookup("a.pdf")
	joined := strings.Join(e.Tags, ",")
	if !strings.Contains(joined, "weird.name") {
		t.Errorf("the tolerated tag was dropped: %v", e.Tags)
	}
}

// A filter query can name a critical tag by quoting it, so the picker's warning
// is a warning, not a dead end.
func TestQuotedQueryReachesCriticalTag(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m.lib.Store.Config.TagTable = append(m.lib.Store.Config.TagTable, meta.Tag{Name: "KV cache"})
	m.lib.Store.Set("a.pdf", &meta.Entry{Title: "A", Tags: []string{"KV cache"}})
	_ = m.lib.Rescan()
	m.rebuild()

	n, err := filter.Parse(`"KV cache"`)
	if err != nil {
		t.Fatalf("a quoted name does not parse: %v", err)
	}
	if got := app.PaperHasTags(m.lib, m.lib.Papers[0], n); !got {
		t.Error("the quoted query did not match the tag")
	}
}

// A filtered view must explain its own match: when the query matched on a tag
// the list normally hides, that tag is shown in red rather than leaving the user
// looking at a bare file name that seems to match nothing.
func TestFilteredRowShowsProblemTag(t *testing.T) {
	m := newTestModel(t, "a.pdf", "b.pdf")
	m.lib.Store.Config.TagTable = append(m.lib.Store.Config.TagTable,
		meta.Tag{Name: "KV cache"})
	m.lib.Store.Set("a.pdf", &meta.Entry{Title: "A", Tags: []string{"KV cache"}})
	m.lib.Store.Set("b.pdf", &meta.Entry{Title: "B"})
	_ = m.lib.Rescan()
	m.rebuild()
	m.ready = true
	m.width, m.height = 100, 20

	// Unfiltered: the unusable tag is hidden.
	row := stripEsc(m.listRow(0, 60))
	if strings.Contains(row, "KV cache") {
		t.Errorf("the unfiltered row shows a problem tag: %q", row)
	}

	// Filtered on it: one row, and it says why.
	m.setFilterText(`"KV cache"`)
	if m.filterErr != nil {
		t.Fatal(m.filterErr)
	}
	if len(m.visible) != 1 {
		t.Fatalf("filter matched %d papers", len(m.visible))
	}
	row = stripEsc(m.listRow(0, 70))
	if !strings.Contains(row, "KV cache") {
		t.Errorf("the filtered row does not explain the match: %q", row)
	}
	if w := len([]rune(row)); w != 70 {
		t.Errorf("row is %d cells, want 70", w)
	}
}

// Only a filtered view shows them; a plain search does not.
func TestSearchRowHidesProblemTag(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m.lib.Store.Config.TagTable = append(m.lib.Store.Config.TagTable,
		meta.Tag{Name: "KV cache"})
	m.lib.Store.Set("a.pdf", &meta.Entry{Title: "Alpha", Tags: []string{"KV cache"}})
	_ = m.lib.Rescan()
	m.quat = "alpha"
	m.rebuild()
	m.ready = true
	m.width, m.height = 100, 20
	if len(m.visible) != 1 {
		t.Fatalf("search matched %d", len(m.visible))
	}
	if row := stripEsc(m.listRow(0, 60)); strings.Contains(row, "KV cache") {
		t.Errorf("a search row shows a problem tag: %q", row)
	}
}

// A name that would be ambiguous on one line is quoted, so "a b" as one tag and
// "a" + "b" as two cannot look the same.
func TestTagSummaryQuotesAmbiguousNames(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"LLM"}, "LLM"},
		{[]string{"a", "b"}, "a b"},
		{[]string{"KV cache"}, `"KV cache"`},
		{[]string{"a|b", "c"}, `"a|b" c`},
		{[]string{"a1", "a2", "a3", "a4"}, "a1 a2 a3+"},
		{[]string{"x y", "a", "b", "c"}, `"x y" a b+`},
	}
	for _, c := range cases {
		if got := tagSummary(c.in); got != c.want {
			t.Errorf("tagSummary(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}
