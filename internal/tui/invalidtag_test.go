package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"non/internal/meta"
)

// unknownTagModel is a library whose paper carries a tag the table rejects.
func unknownTagModel(t *testing.T) *Model {
	t.Helper()
	m := newTestModel(t, "a.pdf")
	m.lib.Store.Config.SetTag(-1, meta.Tag{Name: "LLM"})
	m.lib.Store.Set("a.pdf", &meta.Entry{Title: "Alpha", Tags: []string{"LLM", "typo"}})
	_ = m.lib.Rescan()
	m.rebuild()
	m.width, m.height = 100, 26
	return m
}

// An unknown tag is hidden from every display, because showing it as if it were
// a real tag would make the warning easy to forget.
func TestUnknownTagHiddenFromList(t *testing.T) {
	m := unknownTagModel(t)
	row := stripEsc(m.listRow(0, 60))
	if strings.Contains(row, "typo") {
		t.Errorf("the list shows an unknown tag: %q", row)
	}
	if !strings.Contains(row, "LLM") {
		t.Errorf("the known tag is missing: %q", row)
	}
}

func TestUnknownTagHiddenFromDetails(t *testing.T) {
	m := unknownTagModel(t)
	out := stripEsc(m.View())
	if strings.Contains(out, "#typo") {
		t.Errorf("the details pane shows an unknown tag:\n%s", out)
	}
	if !strings.Contains(out, "#LLM") {
		t.Errorf("the details pane is missing the known tag:\n%s", out)
	}
}

// A paper whose only tag is unknown says so, rather than looking untagged.
func TestAllTagsUnknownIsReported(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m.lib.Store.Set("a.pdf", &meta.Entry{Title: "Alpha", Tags: []string{"typo"}})
	_ = m.lib.Rescan()
	m.rebuild()
	m.width, m.height = 100, 26
	out := stripEsc(m.View())
	if !strings.Contains(out, "not in the tag table") {
		t.Errorf("no hint that the tags are unusable:\n%s", out)
	}
}

// The form lists the unknown tag, marked, so it can be resolved there.
func TestMetaFormMarksUnknownTag(t *testing.T) {
	m := unknownTagModel(t)
	m.ready = true
	m = key(m, "m")
	f := m.metaForm
	found := -1
	for i, tt := range f.tags {
		if tt.Name == "typo" {
			found = i
			if !f.tagBad[i] {
				t.Error("the unknown tag is not marked bad")
			}
			if !f.tagOn[i] {
				t.Error("the unknown tag should start selected, so unticking it removes it")
			}
		} else if f.tagBad[i] {
			t.Errorf("%q was wrongly marked bad", tt.Name)
		}
	}
	if found < 0 {
		t.Fatal("the unknown tag does not appear in the form at all")
	}
	// Walk down to the tags row: the form scrolls, so the checklist is not on
	// screen while the cursor is still in the upper fields.
	f.cur = rowIndex(m, rowTags)
	m.focusMetaRow()
	out := stripEsc(m.View())
	if !strings.Contains(out, "[!] typo") {
		t.Errorf("the unknown tag is not marked in the view:\n%s", out)
	}
}

// Saving while an unknown tag is selected is refused, with an explanation.
func TestMetaFormRefusesToSaveUnknownTag(t *testing.T) {
	m := unknownTagModel(t)
	m = key(m, "m")
	m = key(m, "ctrl+s")
	if m.mode != modeMetaForm {
		t.Fatal("the form closed with an unknown tag selected")
	}
	if !strings.Contains(m.metaForm.status, "unusable tag") {
		t.Errorf("status = %q", m.metaForm.status)
	}
	if !strings.Contains(m.metaForm.status, "typo") {
		t.Errorf("the message does not name the tag: %q", m.metaForm.status)
	}
	// The store is untouched.
	e, _ := m.lib.Store.Lookup("a.pdf")
	if len(e.Tags) != 2 {
		t.Errorf("tags changed: %v", e.Tags)
	}
}

// Unticking it makes the form saveable, and the tag is then gone.
func TestMetaFormUntickingRemovesUnknownTag(t *testing.T) {
	m := unknownTagModel(t)
	m = key(m, "m")
	f := m.metaForm
	f.cur = rowIndex(m, rowTags)
	m.focusMetaRow()
	// Move to the unknown tag and untick it.
	for i, tt := range f.tags {
		if tt.Name == "typo" {
			f.rows[f.cur].tagPos = i
		}
	}
	m = key(m, " ")
	m = key(m, "ctrl+s")
	if m.mode != modeBrowse {
		t.Fatalf("still in the form: %q", m.metaForm.status)
	}
	e, _ := m.lib.Store.Lookup("a.pdf")
	if len(e.Tags) != 1 || e.Tags[0] != "LLM" {
		t.Errorf("tags = %v, want [LLM]", e.Tags)
	}
	if got := m.lib.Store.UnknownTags(); len(got) != 0 {
		t.Errorf("still unknown: %v", got)
	}
}

// A duplicate typed into the add-tag row still selects the tag -- the intent is
// fulfilled -- but says plainly that it already existed. A different case is the
// same tag.
func TestMetaFormDuplicateTagWarnsAndSelects(t *testing.T) {
	for _, name := range []string{"LLM", "llm", "LlM"} {
		m := unknownTagModel(t)
		m = key(m, "m")
		f := m.metaForm
		f.cur = rowIndex(m, rowNewTag)
		m.focusMetaRow()
		f.rows[f.cur].input.SetValue(name)
		m = key(m, "enter")

		if !strings.Contains(f.status, "already exists") {
			t.Errorf("%q: no warning, status = %q", name, f.status)
		}
		if !f.statusK {
			t.Errorf("%q: the message should read as a warning", name)
		}
		// The existing tag is now selected.
		found := false
		for i, tt := range f.tags {
			if tt.Name == "LLM" && f.tagOn[i] {
				found = true
			}
		}
		if !found {
			t.Errorf("%q: the existing tag was not selected", name)
		}
		// And nothing was duplicated in the table.
		if got := m.lib.Store.Config.TagNames(); len(got) != 1 {
			t.Errorf("%q: table = %v", name, got)
		}
		// The row is cleared, so the same text is not submitted twice.
		if got := f.rows[f.cur].input.Value(); got != "" {
			t.Errorf("%q: the add row still holds %q", name, got)
		}
	}
}

// The settings tag form treats a duplicate the same way: it says the tag exists
// and returns to the list with that row selected. Nothing is duplicated.
func TestSettingsDuplicateTagCase(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m.lib.Store.Config.SetTag(-1, meta.Tag{Name: "LLM"})
	m.openSettings()
	m.settings.page = pageTags
	m.refreshTags()
	m = key(m, "a")
	for _, name := range []string{"LLM", "llm", "LlM"} {
		m.settings.form[0].input.SetValue(name)
		m = key(m, "ctrl+s")
		if m.settings.page != pageTags {
			t.Fatalf("%q: still in the form", name)
		}
		if !strings.Contains(m.settings.status, "already exists") {
			t.Errorf("%q: status = %q", name, m.settings.status)
		}
		// Back on the tag list, with the existing tag selected.
		if len(m.settings.tags) != 1 || m.settings.cursor != 0 {
			t.Errorf("%q: tags=%d cursor=%d", name, len(m.settings.tags), m.settings.cursor)
		}
		// Ready to try again.
		m = key(m, "a")
	}
	if got := len(m.lib.Store.Config.TagTable); got != 1 {
		t.Errorf("table has %d rows", got)
	}
}

// Starting in a directory with an unknown tag warns immediately.
func TestOpenWarnsAboutUnknownTags(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.pdf"), []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw := `{"version":1,"entries":{"a.pdf":{"title":"A","tags":["typo"]}},
	         "config":{"tags":[{"name":"LLM"}]}}`
	if err := os.WriteFile(filepath.Join(dir, meta.StoreFile), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	m.ready = true // no terminal in a test, so no WindowSizeMsg arrives
	m.width, m.height = 100, 26
	if !m.statusK {
		t.Error("the warning is not marked as a problem")
	}
	if !strings.Contains(m.status, "typo") {
		t.Errorf("status = %q", m.status)
	}
	if !strings.Contains(stripEsc(m.View()), "typo") {
		t.Error("the warning is not visible")
	}
}

// The $EDITOR path refuses an unknown tag and keeps the user's text.
func TestEditorRejectsUnknownTagKeepsText(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m.lib.Store.Config.SetTag(-1, meta.Tag{Name: "LLM"})
	_ = m.lib.Rescan()
	m.rebuild()

	path := filepath.Join(t.TempDir(), "meta.md")
	src := "# Edited\n\n- Tags: LLM, bogus\n\n# Comment\nhi\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	mm, _ := m.Update(editorDoneMsg{name: "a.pdf", path: path})
	m = mm.(*Model)

	if e, _ := m.lib.Store.Lookup("a.pdf"); e != nil && e.Title == "Edited" {
		t.Fatal("the metadata was written despite an unknown tag")
	}
	if !m.statusK {
		t.Errorf("no error shown, status = %q", m.status)
	}
	if !strings.Contains(m.status, "bogus") {
		t.Errorf("the message does not name the tag: %q", m.status)
	}
	if !strings.Contains(m.status, path) {
		t.Errorf("the message does not say where the text is: %q", m.status)
	}
	if back, err := os.ReadFile(path); err != nil || string(back) != src {
		t.Errorf("the text was not kept: %v %q", err, back)
	}
}

// Unparseable text is handled the same way: nothing written, text kept.
func TestEditorRejectsMangledTextKeepsIt(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	path := filepath.Join(t.TempDir(), "meta.md")
	src := "no title here\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	mm, _ := m.Update(editorDoneMsg{name: "a.pdf", path: path})
	m = mm.(*Model)
	if !strings.Contains(m.status, "title") {
		t.Errorf("status = %q", m.status)
	}
	if !strings.Contains(m.status, path) {
		t.Errorf("the message does not say where the text is: %q", m.status)
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("the text was discarded")
	}
}

// A rejected edit must not leave a "saved" illusion anywhere: the store keeps
// what it had.
func TestEditorRejectionLeavesStoreAlone(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m.lib.Store.Set("a.pdf", &meta.Entry{Title: "Original", Tags: []string{}})
	_ = m.lib.Rescan()
	m.rebuild()
	before, _ := m.lib.Store.Lookup("a.pdf")

	path := filepath.Join(t.TempDir(), "meta.md")
	if err := os.WriteFile(path, []byte("# Different\n\n- Tags: nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mm, _ := m.Update(editorDoneMsg{name: "a.pdf", path: path})
	m = mm.(*Model)
	after, _ := m.lib.Store.Lookup("a.pdf")
	if after.Title != before.Title {
		t.Errorf("title changed from %q to %q", before.Title, after.Title)
	}
}

// A successful edit still removes its temp file.
func TestEditorAcceptanceRemovesTempFile(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m.lib.Store.Config.SetTag(-1, meta.Tag{Name: "LLM"})
	_ = m.lib.Rescan()
	m.rebuild()
	path := filepath.Join(t.TempDir(), "meta.md")
	if err := os.WriteFile(path, []byte("# Fine\n\n- Tags: LLM\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mm, _ := m.Update(editorDoneMsg{name: "a.pdf", path: path})
	m = mm.(*Model)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("the temp file was not removed after a successful save")
	}
	if e, _ := m.lib.Store.Lookup("a.pdf"); e.Title != "Fine" {
		t.Errorf("title = %q", e.Title)
	}
}
