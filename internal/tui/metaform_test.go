package tui

import (
	"strings"
	"testing"

	"non/internal/meta"
)

// inlineModel is a model configured to use the built-in metadata form.
func inlineModel(t *testing.T, tags ...string) *Model {
	t.Helper()
	m := newTestModel(t, "a.pdf")
	m.lib.Store.Set("a.pdf", &meta.Entry{
		Title: "Attention", Author: "Vaswani", Abstract: "seq2seq",
		Tags: []string{"kvcache"},
	})
	_ = m.lib.Rescan()
	m.lib.Store.Config.UseEditor = false
	for _, tag := range tags {
		m.lib.Store.Config.SetTag(-1, meta.Tag{Name: tag})
	}
	m.rebuild()
	return m
}

func TestInlineEditorOpens(t *testing.T) {
	m := inlineModel(t, "kvcache", "serving")
	m = key(m, "m")
	if m.mode != modeMetaForm {
		t.Fatalf("mode = %v, want the metadata form", m.mode)
	}
	f := m.metaForm
	if f.rows[0].input.Value() != "Attention" {
		t.Errorf("title = %q", f.rows[0].input.Value())
	}
	if f.rows[1].input.Value() != "Vaswani" {
		t.Errorf("author = %q", f.rows[1].input.Value())
	}
	// The already selected tag is checked.
	checked := 0
	for i, on := range f.tagOn {
		if on {
			checked++
			if f.tags[i].Name != "kvcache" {
				t.Errorf("wrong tag checked: %q", f.tags[i].Name)
			}
		}
	}
	if checked != 1 {
		t.Errorf("checked = %d, want 1", checked)
	}
}

func TestEditorSettingChoosesEditor(t *testing.T) {
	m := inlineModel(t)
	m.lib.Store.Config.UseEditor = true
	cmd := m.editMetaCmd()
	if m.mode == modeMetaForm {
		t.Error("the built-in form must not open when the editor setting is on")
	}
	if cmd == nil {
		t.Error("expected the editor command")
	}
}

func TestFreshInstallUsesBuiltInForm(t *testing.T) {
	// The default matters: a new user gets the form that enforces the tags.
	m := newTestModel(t, "a.pdf")
	if m.lib.Store.Config.UseEditor {
		t.Fatal("$EDITOR should be off by default")
	}
	m.editMetaCmd()
	if m.mode != modeMetaForm {
		t.Errorf("mode = %v, want the built-in form", m.mode)
	}
}

func TestInlineEditorEditsAndSaves(t *testing.T) {
	m := inlineModel(t, "kvcache", "serving")
	m = key(m, "m")
	m.metaForm.rows[0].input.SetValue("Attention Is All You Need")
	m.metaForm.rows[3].area.SetValue("re-read 3.2")
	m = key(m, "ctrl+s")

	if m.mode != modeBrowse {
		t.Fatalf("mode = %v", m.mode)
	}
	e, _ := m.lib.Store.Lookup("a.pdf")
	if e.Title != "Attention Is All You Need" {
		t.Errorf("title = %q", e.Title)
	}
	if e.Comment != "re-read 3.2" {
		t.Errorf("comment = %q", e.Comment)
	}
	// The store on disk must match, not just the in-memory copy.
	again, err := meta.Load(m.dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := again.Lookup("a.pdf"); got.Title != e.Title {
		t.Errorf("saved title = %q", got.Title)
	}
}

func TestInlineEditorRequiresTitle(t *testing.T) {
	m := inlineModel(t)
	m = key(m, "m")
	m.metaForm.rows[0].input.SetValue("   ")
	m = key(m, "ctrl+s")
	if m.mode != modeMetaForm {
		t.Error("form closed without a title")
	}
	if !m.metaForm.statusK {
		t.Error("no error reported")
	}
	if e, _ := m.lib.Store.Lookup("a.pdf"); e.Title != "Attention" {
		t.Errorf("entry changed: %q", e.Title)
	}
}

func TestInlineEditorCancels(t *testing.T) {
	m := inlineModel(t)
	m = key(m, "m")
	m.metaForm.rows[0].input.SetValue("Changed")
	m = key(m, "esc")
	if m.mode != modeBrowse {
		t.Fatalf("mode = %v", m.mode)
	}
	if e, _ := m.lib.Store.Lookup("a.pdf"); e.Title != "Attention" {
		t.Errorf("escape saved changes: %q", e.Title)
	}
}

func TestInlineEditorTagChecklistIsConstrained(t *testing.T) {
	// The point of the checklist: a tag cannot be invented by typing into a
	// free text field, it has to come from the global table.
	m := inlineModel(t, "kvcache", "serving")
	m = key(m, "m")
	m.metaForm.cur = rowIndex(m, rowTags)
	m.focusMetaRow()

	m = key(m, "right") // second tag
	m = key(m, " ")     // check it
	m = key(m, "ctrl+s")

	e, _ := m.lib.Store.Lookup("a.pdf")
	if len(e.Tags) != 2 {
		t.Fatalf("tags = %v", e.Tags)
	}
	for _, tag := range e.Tags {
		if _, ok := m.lib.Store.Config.Tag(tag); !ok {
			t.Errorf("tag %q is not in the table", tag)
		}
	}
}

func TestInlineEditorAddsTagToTable(t *testing.T) {
	m := inlineModel(t, "kvcache")
	m = key(m, "m")
	m.metaForm.cur = rowIndex(m, rowNewTag)
	m.focusMetaRow()
	m.metaForm.rows[m.metaForm.cur].input.SetValue("brand_new")
	m = key(m, "enter")

	if _, ok := m.lib.Store.Config.Tag("brand_new"); !ok {
		t.Fatal("a tag typed in the add row must be added to the table")
	}
	// ...and it ends up selected, so saving keeps it on the paper.
	m = key(m, "ctrl+s")
	e, _ := m.lib.Store.Lookup("a.pdf")
	found := false
	for _, tag := range e.Tags {
		if tag == "brand_new" {
			found = true
		}
	}
	if !found {
		t.Errorf("tags = %v", e.Tags)
	}
}

func TestInlineEditorKeepsExtraFields(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m.lib.Store.Set("a.pdf", &meta.Entry{Title: "T"})
	entry, _ := m.lib.Store.Lookup("a.pdf")
	entry.SetField("DOI", "10.1/x")
	entry.SetField("Year", "2017")
	_ = m.lib.Store.Save()
	_ = m.lib.Rescan()
	m.lib.Store.Config.UseEditor = false
	m.rebuild()

	m = key(m, "m")
	// Extra rows are shown and editable.
	if rowIndex(m, rowExtra) < 0 {
		t.Fatal("extra fields not shown in the form")
	}
	m.metaForm.rows[0].input.SetValue("New title")
	m = key(m, "ctrl+s")

	got, _ := m.lib.Store.Lookup("a.pdf")
	if got.FieldString("DOI") != "10.1/x" || got.FieldString("Year") != "2017" {
		t.Errorf("extra fields lost: %v", got.Fields)
	}
}

func TestInlineEditorClearingExtraFieldRemovesIt(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m.lib.Store.Set("a.pdf", &meta.Entry{Title: "T"})
	entry, _ := m.lib.Store.Lookup("a.pdf")
	entry.SetField("DOI", "10.1/x")
	_ = m.lib.Store.Save()
	_ = m.lib.Rescan()
	m.lib.Store.Config.UseEditor = false
	m.rebuild()

	m = key(m, "m")
	for i := range m.metaForm.rows {
		if m.metaForm.rows[i].kind == rowExtra {
			m.metaForm.rows[i].area.SetValue("")
		}
	}
	m = key(m, "ctrl+s")
	got, _ := m.lib.Store.Lookup("a.pdf")
	if got.HasField("DOI") {
		t.Errorf("cleared field kept: %v", got.Fields)
	}
}

func TestInlineEditorAddsNewField(t *testing.T) {
	m := inlineModel(t)
	m = key(m, "m")
	f := m.metaForm
	f.cur = rowIndex(m, rowNewField)
	m.focusMetaRow()
	f.rows[f.cur].input.SetValue("doi=10.1234/abc")
	m = key(m, "enter")

	// A row appears for the field and it is saved under its own key.
	found := -1
	for i, r := range m.metaForm.rows {
		if r.kind == rowExtra && r.label == "doi" {
			found = i
		}
	}
	if found < 0 {
		t.Fatal("no row added for the new field")
	}
	if m.metaForm.rows[found].area.Value() != "10.1234/abc" {
		t.Errorf("value = %q", m.metaForm.rows[found].area.Value())
	}
	m = key(m, "ctrl+s")
	e, _ := m.lib.Store.Lookup("a.pdf")
	if e.FieldString("doi") != "10.1234/abc" {
		t.Errorf("doi = %q", e.FieldString("doi"))
	}
}

func TestInlineEditorNewFieldNeedsEquals(t *testing.T) {
	m := inlineModel(t)
	m = key(m, "m")
	f := m.metaForm
	f.cur = rowIndex(m, rowNewField)
	m.focusMetaRow()
	f.rows[f.cur].input.SetValue("justaname")
	m = key(m, "enter")
	if !f.statusK {
		t.Error("a field without = should be refused")
	}
	if rowIndex(m, rowExtra) >= 0 {
		t.Error("a malformed field was added anyway")
	}
}

func TestMetaFormViewRenders(t *testing.T) {
	m := inlineModel(t, "kvcache", "serving", "ml-systems")
	for _, size := range [][2]int{{100, 30}, {60, 20}, {120, 45}} {
		m.width, m.height = size[0], size[1]
		m.mode = modeBrowse
		m = key(m, "m")
		out := m.View()
		if !strings.Contains(out, "edit metadata") {
			t.Errorf("%dx%d: form not rendered", size[0], size[1])
		}
		if got := len(strings.Split(out, "\n")); got != m.height {
			t.Errorf("%dx%d: %d lines, want %d", size[0], size[1], got, m.height)
		}
		for i := 0; i < len(m.metaForm.rows); i++ {
			m = key(m, "tab")
			out = m.View()
			if got := len(strings.Split(out, "\n")); got != m.height {
				t.Errorf("%dx%d row %d: %d lines, want %d", size[0], size[1], i, got, m.height)
			}
		}
		m = key(m, "esc")
	}
}

// rowIndex returns the index of the first row of the given kind.
func rowIndex(m *Model, kind metaRowKind) int {
	for i, r := range m.metaForm.rows {
		if r.kind == kind {
			return i
		}
	}
	return -1
}
