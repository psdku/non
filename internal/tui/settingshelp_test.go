package tui

import (
	"strings"
	"testing"
)

// The description row must sit on a fixed row of the modal: moving the cursor
// may change the text, never the layout.
func TestSettingsDescriptionDoesNotShiftLayout(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m.width, m.height = 100, 30
	m.openSettings()

	var first []string
	for i := 0; i < len(m.settings.fields()); i++ {
		lines := strings.Split(stripEsc(m.View()), "\n")
		if first == nil {
			first = lines
		} else if len(lines) != len(first) {
			t.Fatalf("cursor %d: view has %d lines, first had %d", i, len(lines), len(first))
		}
		// The box must be in the same place.
		top, bottom, _ := modalBox(t, m)
		top0, bottom0 := boxBounds(t, first)
		if top != top0 || bottom != bottom0 {
			t.Fatalf("cursor %d: box moved from %d-%d to %d-%d",
				i, top0, bottom0, top, bottom)
		}
		m = key(m, "j")
	}
}

// boxBounds finds the modal box rows in an already rendered view.
func boxBounds(t *testing.T, lines []string) (top, bottom int) {
	t.Helper()
	top, bottom = -1, -1
	for i, l := range lines {
		if top < 0 && strings.Contains(l, "settings") && strings.Contains(l, "┌") {
			top = i
		}
		if top >= 0 && strings.Contains(l, "└") && strings.Contains(l, "┘") {
			bottom = i
			break
		}
	}
	return top, bottom
}

// Whatever the cursor is on, the description row says something: a blank row is
// a missing explanation.
func TestSettingsEveryFieldHasDescription(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m.width, m.height = 100, 40
	m.openSettings()
	for i, f := range m.settings.fields() {
		if strings.TrimSpace(f.help) == "" {
			t.Errorf("setting %q (%d) has no description", f.label, i)
		}
	}
	// And it is actually rendered, changing with the cursor.
	seen := map[string]bool{}
	for i := 0; i < len(m.settings.fields()); i++ {
		desc := stripEsc(m.settingsDescription(80))
		if strings.TrimSpace(desc) == "" {
			t.Fatalf("cursor %d renders no description", i)
		}
		seen[desc] = true
		m = key(m, "j")
	}
	if len(seen) < 2 {
		t.Errorf("the description never changed while moving: %v", seen)
	}
}

// The description row is not one of the selectable rows: it must not be counted
// in the box height, or the box would grow a row.
func TestSettingsDescriptionIsOutsideTheList(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m.openSettings()
	rows := m.settingsBodyRows()
	if want := len(m.settings.fields()) + 1; rows != want {
		t.Errorf("settingsBodyRows = %d, want %d (one row per setting)", rows, want)
	}
}

// Pages with their own lists get a description too, from the current entry.
func TestSettingsListPagesDescribeTheCursor(t *testing.T) {
	m := settingsWithList(t, pageTags, 3)
	m.width, m.height = 100, 30
	m.settings.tags[0].Description = "a tag"
	m.settings.cursor = 0
	if got := stripEsc(m.settingsDescription(60)); !strings.Contains(got, "a tag") {
		t.Errorf("tag description = %q", got)
	}
	// An empty description explains how to add one instead of showing nothing.
	m.settings.tags[0].Description = ""
	if got := stripEsc(m.settingsDescription(60)); !strings.Contains(got, "enter") {
		t.Errorf("empty description = %q", got)
	}

	m = settingsWithList(t, pageTools, 3)
	m.width, m.height = 100, 30
	m.settings.cursor = 0
	if got := stripEsc(m.settingsDescription(60)); !strings.Contains(got, "run item00") {
		t.Errorf("tool description = %q", got)
	}
}
