package tui

import (
	"strings"
	"testing"

	"non/internal/meta"
)

// Every rendered line must be exactly the terminal width, otherwise a panel
// border shifts and the layout looks broken.
func TestPanelsAreExactWidth(t *testing.T) {
	m := newTestModel(t, "a.pdf", "b.pdf", "c.pdf")
	m.lib.Store.Set("a.pdf", &meta.Entry{
		Title: strings.Repeat("Long ", 30), Abstract: strings.Repeat("word ", 300),
	})
	m.lib.Rescan()
	m.rebuild()

	for _, size := range [][2]int{{40, 12}, {60, 18}, {100, 30}, {160, 40}} {
		m.width, m.height = size[0], size[1]
		m.mode = modeBrowse
		out := m.View()
		lines := strings.Split(out, "\n")
		if len(lines) != size[1] {
			t.Errorf("%dx%d: %d lines", size[0], size[1], len(lines))
		}
		for i, l := range lines {
			if w := len([]rune(stripEsc(l))); w != size[0] {
				t.Errorf("%dx%d line %d = %d cells, want %d: %q",
					size[0], size[1], i, w, size[0], stripEsc(l))
			}
		}
	}
}

// The panes must be siblings: the list's right border and the details pane's
// left border are adjacent, and their top rules share a row.
func TestPanesAreSideBySide(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	out := strings.Split(m.View(), "\n")
	if len(out) < 2 {
		t.Fatal("view too short")
	}
	top := stripEsc(out[1])
	if !strings.HasPrefix(top, "┌") {
		t.Errorf("row 1 is not a panel top rule: %q", top)
	}
	// Both panes start their top rule on the same row, so there are two "┌".
	if c := strings.Count(top, "┌"); c != 2 {
		t.Errorf("row 1 has %d top rules, want 2 (two panes): %q", c, top)
	}
	// The bottom row closes both.
	bottom := stripEsc(out[len(out)-2])
	if c := strings.Count(bottom, "└"); c != 2 {
		t.Errorf("bottom row has %d bottom rules, want 2: %q", c, bottom)
	}
}

// Titles are written into the top border, as yazi labels its panels.
func TestPanelTitles(t *testing.T) {
	m := newTestModel(t, "a.pdf", "b.pdf")
	top := stripEsc(strings.Split(m.View(), "\n")[1])
	for _, want := range []string{"2 papers", "details"} {
		if !strings.Contains(top, want) {
			t.Errorf("top border is missing %q: %q", want, top)
		}
	}
	// The count reflects a filtered list.
	m.quat = "a.pdf"
	m.rebuild()
	top = stripEsc(strings.Split(m.View(), "\n")[1])
	if !strings.Contains(top, "1/2") {
		t.Errorf("filtered title = %q, want 1/2", top)
	}
}

// The current line uses reverse video and nothing else, so it reads as a bar.
func TestCursorIsReverseVideo(t *testing.T) {
	m := newTestModel(t, "a.pdf", "b.pdf")
	m.rebuild()
	out := m.View()
	if !strings.Contains(out, "\x1b[7m") {
		t.Error("no reverse-video escape in the view")
	}
	// The bar must span the pane: the styled row has no padding outside it.
	row := m.listRow(0, 20)
	if !strings.HasPrefix(row, "\x1b[7m") {
		t.Errorf("cursor row does not start with reverse video: %q", row)
	}
	if w := len([]rune(stripEsc(row))); w != 20 {
		t.Errorf("cursor row is %d cells, want 20", w)
	}
}

// The status bar starts with a mode block, like yazi's mode indicator.
func TestStatusBarMode(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	if !strings.Contains(m.viewFooter(m.width), "NORMAL") {
		t.Error("no mode block in the status bar")
	}
	m = key(m, "/")
	if !strings.Contains(m.viewFooter(m.width), "SEARCH") {
		t.Error("search mode not shown")
	}
	m = key(m, "esc")
	m = key(m, "enter")
	if !strings.Contains(m.viewFooter(m.width), "ACTION") {
		t.Error("action mode not shown")
	}
}

// A modal must float over the browser: its first row must not be the terminal's
// first row, and the rows above it must still be the browser.
func TestModalFloatsOverBrowser(t *testing.T) {
	m := newTestModel(t, "a.pdf", "b.pdf")
	m.width, m.height = 100, 30
	plain := strings.Split(stripEsc(m.View()), "\n")

	m.settings = nil
	m.mode = modeSettings
	m.openSettings()
	withModal := strings.Split(stripEsc(m.View()), "\n")

	if len(withModal) != len(plain) {
		t.Fatalf("modal changed the height: %d vs %d", len(withModal), len(plain))
	}
	// The modal box must start below the top, and end above the bottom.
	topRow := -1
	for i, l := range withModal {
		if strings.Contains(l, "settings ─") {
			topRow = i
			break
		}
	}
	if topRow < 1 {
		t.Fatalf("modal top row = %d, want it below the header", topRow)
	}
	// Rows outside the box still show the browser (the header row is intact).
	if !strings.Contains(withModal[0], "non") {
		t.Errorf("header lost under the modal: %q", withModal[0])
	}
	if !strings.Contains(withModal[len(withModal)-1], "NORMAL") {
		t.Errorf("status bar lost under the modal: %q", withModal[len(withModal)-1])
	}
	// Something of the browser is visible beside the box.
	if strings.Contains(withModal[topRow], "papers ─") &&
		strings.Count(withModal[topRow], "┌") < 2 {
		t.Errorf("browser panes are gone behind the modal: %q", withModal[topRow])
	}
}

// The built-in form is a floating panel too, not a separate screen.
func TestMetaFormFloats(t *testing.T) {
	m := inlineModel(t)
	m.width, m.height = 100, 30
	m = key(m, "m")
	out := strings.Split(stripEsc(m.View()), "\n")
	if !strings.Contains(out[0], "non") {
		t.Errorf("header lost under the form: %q", out[0])
	}
	found := false
	for i, l := range out {
		if strings.Contains(l, "edit metadata") {
			if i < 1 {
				t.Errorf("form is at the very top: %q", l)
			}
			found = true
		}
	}
	if !found {
		t.Error("form title not found")
	}
}

// A menu is drawn as a box with side rules, over the list.
func TestMenuIsABox(t *testing.T) {
	m := newTestModel(t, "a.pdf", "b.pdf")
	m = key(m, "enter")
	out := strings.Split(stripEsc(m.View()), "\n")
	joined := strings.Join(out, "\n")
	if !strings.Contains(joined, "╭ actions") {
		t.Errorf("menu has no titled top rule:\n%s", joined)
	}
	if !strings.Contains(joined, "╰") {
		t.Errorf("menu has no bottom rule:\n%s", joined)
	}
	for _, l := range out {
		if strings.Contains(l, "│ Open with") {
			// A menu row still has to fill the terminal line exactly, or the
			// pane border to its right would slide.
			if w := len([]rune(l)); w != m.width {
				t.Errorf("menu row is %d cells wide, want %d: %q", w, m.width, l)
			}
		}
	}
}
