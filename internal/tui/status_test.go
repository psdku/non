package tui

import (
	"strings"
	"testing"
)

// keymapVisible reports whether the modal's footer shows the keymap rather than
// a message.
func keymapVisible(m *Model) bool {
	out := stripEsc(m.View())
	return strings.Contains(out, "esc close") || strings.Contains(out, "esc back")
}

// A settings message is feedback about one action; the next keypress must
// dismiss it and bring back the keymap.
func TestSettingsMessageClearsOnAnyKey(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m.openSettings()
	m.width, m.height = 100, 30

	// Every key that is not meaningful on the root page must still clear it.
	for _, k := range []string{"x", "z", "ctrl+a", "1", "shift+tab"} {
		m.settings.status = "saved"
		if !strings.Contains(stripEsc(m.View()), "saved") {
			t.Fatalf("%q: the message is not shown to begin with", k)
		}
		m = key(m, k)
		if m.settings.status != "" {
			t.Errorf("%q did not clear the message: %q", k, m.settings.status)
		}
		if !keymapVisible(m) {
			t.Errorf("%q did not restore the keymap:\n%s", k, stripEsc(m.View()))
		}
	}
}

// Movement clears it too, and the description row updates as usual.
func TestSettingsMessageClearsOnMovement(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m.openSettings()
	m.width, m.height = 100, 30
	for _, k := range []string{"j", "k", "down", "up"} {
		m.settings.status = "saved"
		m = key(m, k)
		if m.settings.status != "" {
			t.Errorf("%q left the message up: %q", k, m.settings.status)
		}
	}
}

// An error is cleared the same way: it has been read.
func TestSettingsErrorClearsOnAnyKey(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m.openSettings()
	m.width, m.height = 100, 30
	m.settings.setErr("something went wrong")
	m = key(m, "x")
	if m.settings.status != "" {
		t.Errorf("the error stayed: %q", m.settings.status)
	}
}

// A key that acts must still report its own result: clearing first and then
// setting is the order that keeps feedback working.
func TestSettingsActionStillReports(t *testing.T) {
	m := settingsWithList(t, pageTools, 2)
	m.width, m.height = 100, 30
	m.settings.cursor = 0
	m = key(m, "d") // delete the first tool
	if m.settings.status == "" {
		t.Fatal("deleting a tool reported nothing")
	}
	if !strings.Contains(m.settings.status, "removed tool") {
		t.Errorf("status = %q", m.settings.status)
	}
	// And the next keypress takes it away.
	m = key(m, "j")
	if m.settings.status != "" {
		t.Errorf("status survived the next key: %q", m.settings.status)
	}
}

// The in-place saving of a toggle also reports, then clears.
func TestSettingsToggleReportsThenClears(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m.openSettings()
	m.width, m.height = 100, 30
	m = key(m, " ") // toggle the first setting
	if m.settings.status == "" {
		t.Fatal("toggling reported nothing")
	}
	m = key(m, "j")
	if m.settings.status != "" {
		t.Errorf("status survived a move: %q", m.settings.status)
	}
}

// The metadata form behaves the same way: its message row gives way to the
// hint line on the next keypress.
func TestMetaFormMessageClearsOnAnyKey(t *testing.T) {
	m := inlineModel(t, "kvcache")
	m.width, m.height = 100, 30
	m = key(m, "m")
	m.metaForm.status = "added tag kvcache"
	if !strings.Contains(stripEsc(m.View()), "added tag") {
		t.Fatal("the message is not shown to begin with")
	}
	for _, k := range []string{"x", "z", "tab"} {
		m.metaForm.status = "added tag kvcache"
		m = key(m, k)
		if m.metaForm.status != "" {
			t.Errorf("%q left the message up: %q", k, m.metaForm.status)
		}
	}
	// The hint line is back.
	if !strings.Contains(stripEsc(m.View()), "esc cancel") {
		t.Errorf("the hint line did not return:\n%s", stripEsc(m.View()))
	}
}

// Adding a tag in the form still reports what it did after clearing.
func TestMetaFormActionStillReports(t *testing.T) {
	m := inlineModel(t, "kvcache")
	m.width, m.height = 100, 30
	m = key(m, "m")
	m.metaForm.cur = rowIndex(m, rowNewTag)
	m.focusMetaRow()
	m.metaForm.rows[m.metaForm.cur].input.SetValue("brand_new")
	m = key(m, "enter")
	if !strings.Contains(m.metaForm.status, "brand_new") {
		t.Errorf("adding a tag reported %q", m.metaForm.status)
	}
	m = key(m, "x")
	if m.metaForm.status != "" {
		t.Errorf("status survived the next key: %q", m.metaForm.status)
	}
}

// A tag row that toggles reports nothing, so the hint line must survive.
func TestMetaFormToggleKeepsHint(t *testing.T) {
	m := inlineModel(t, "kvcache")
	m.width, m.height = 100, 30
	m = key(m, "m")
	idx := rowIndex(m, rowTags)
	if idx < 0 {
		t.Skip("no tag row in this fixture")
	}
	m.metaForm.cur = idx
	m.focusMetaRow()
	m = key(m, " ") // toggle the tag off
	if m.metaForm.status != "" {
		t.Errorf("toggling a tag set a message: %q", m.metaForm.status)
	}
	if !strings.Contains(stripEsc(m.View()), "esc cancel") {
		t.Errorf("the hint line is missing:\n%s", stripEsc(m.View()))
	}
}
