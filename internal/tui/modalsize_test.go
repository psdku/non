package tui

import (
	"fmt"
	"strings"
	"testing"

	"non/internal/meta"
)

// settingsWithList opens the settings modal on a page with n items.
func settingsWithList(t *testing.T, page setPage, n int) *Model {
	t.Helper()
	m := newTestModel(t, "a.pdf")
	c := &m.lib.Store.Config
	c.Tools = nil
	c.TagTable = nil
	for i := range n {
		name := fmt.Sprintf("item%02d", i)
		if page == pageTags {
			c.TagTable = append(c.TagTable, meta.Tag{Name: name, Description: "a tag"})
		} else {
			c.Tools = append(c.Tools, meta.Tool{Name: name, Command: "run " + name, Type: meta.ToolGUI})
		}
	}
	m.openSettings()
	// The list pages read a snapshot taken when the page opens, so populate it
	// the way the real key handling does.
	m.settings.page = page
	switch page {
	case pageTags:
		m.refreshTags()
	case pageTools:
		m.refreshTools()
	}
	return m
}

// modalBox returns the first and last row of the modal box in a rendered view,
// so its height can be measured from what the user actually sees.
func modalBox(t *testing.T, m *Model) (top, bottom int, lines []string) {
	t.Helper()
	lines = strings.Split(stripEsc(m.View()), "\n")
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
	return top, bottom, lines
}

// The list pages must show their content instead of being clipped to one or two
// rows: this was the reported bug.
func TestModalListShowsManyRows(t *testing.T) {
	for _, page := range []setPage{pageTools, pageTags} {
		m := settingsWithList(t, page, 10)
		m.width, m.height = 100, 40
		top, bottom, _ := modalBox(t, m)
		if top < 0 || bottom < 0 {
			t.Fatalf("%v: modal box not found", page)
		}
		height := bottom - top + 1
		// 1 border + 1 column head + 10 rows + separator + hint + 1 border.
		if height < 14 {
			t.Errorf("%v modal is %d rows tall, want at least 14 for 10 items", page, height)
		}
		// Every item must be on screen.
		out := stripEsc(m.View())
		missing := 0
		for i := range 10 {
			if !strings.Contains(out, fmt.Sprintf("item%02d", i)) {
				missing++
			}
		}
		if missing > 0 {
			t.Errorf("%v: %d of 10 items are clipped", page, missing)
		}
	}
}

// On a tall terminal the dialog must still leave the browser visible rather
// than swallowing the screen.
func TestModalDoesNotFillTallScreen(t *testing.T) {
	m := settingsWithList(t, pageTags, 40)
	m.width, m.height = 120, 60
	top, bottom, _ := modalBox(t, m)
	if top < 0 || bottom < 0 {
		t.Fatal("modal box not found")
	}
	if height := bottom - top + 1; height > m.height*4/5 {
		t.Errorf("modal is %d rows of %d, want at most 4/5", height, m.height)
	}
	if top < 1 {
		t.Errorf("modal starts at row %d, want the header visible", top)
	}
	if bottom > m.height-2 {
		t.Errorf("modal ends at row %d, want the status bar visible", bottom)
	}
}

// The root page is a short form: the box must be sized to it, not stretched.
func TestModalRootStaysCompact(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m.width, m.height = 120, 60
	m.openSettings()
	top, bottom, _ := modalBox(t, m)
	if top < 0 || bottom < 0 {
		t.Fatal("modal box not found")
	}
	height := bottom - top + 1
	// 6 settings + help line + borders/separator/hint, and nothing more.
	if height > 14 {
		t.Errorf("root modal is %d rows, want a compact form (<=14)", height)
	}
}

// A tiny terminal must still produce a usable, bounded box.
func TestModalOnSmallTerminal(t *testing.T) {
	m := settingsWithList(t, pageTags, 30)
	m.width, m.height = 30, 10
	out := stripEsc(m.View())
	lines := strings.Split(out, "\n")
	if len(lines) > 10 {
		t.Errorf("view is %d lines on a 10 row terminal", len(lines))
	}
	for i, l := range lines {
		if w := len([]rune(l)); w > 30 {
			t.Errorf("line %d is %d cells on a 30 column terminal: %q", i, w, l)
		}
	}
}

// Scrolling the list must reach the last item rather than stopping early.
func TestModalListScrollsToEnd(t *testing.T) {
	m := settingsWithList(t, pageTags, 40)
	m.width, m.height = 100, 24
	// Walk to the last tag with the model's own key handling.
	for range 39 {
		m = key(m, "j")
	}
	if m.settings.cursor != 39 {
		t.Fatalf("cursor = %d, want 39", m.settings.cursor)
	}
	out := stripEsc(m.View())
	if !strings.Contains(out, "item39") {
		t.Errorf("the last item is not on screen after scrolling:\n%s", out)
	}
}
