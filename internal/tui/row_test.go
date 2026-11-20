package tui

import (
	"strings"
	"testing"

	"non/internal/meta"
)

// The list shows the file name and its tags, and nothing else: the title and
// author are already in the details pane, and repeating them on every row made
// the list unreadable.
func TestListRowIsNameAndTags(t *testing.T) {
	m := newTestModel(t, "1706.03762v7.pdf")
	m.lib.Store.Set("1706.03762v7.pdf", &meta.Entry{
		Title:  "Attention Is All You Need",
		Author: "Ashish Vaswani",
		Tags:   []string{"attention", "LLM"},
	})
	_ = m.lib.Rescan()
	m.rebuild()

	row := stripEsc(m.listRow(0, 60))
	if !strings.HasPrefix(row, " 1706.03762v7.pdf") {
		t.Errorf("row does not start with the file name: %q", row)
	}
	if !strings.Contains(row, "attention LLM") {
		t.Errorf("tags missing: %q", row)
	}
	if strings.Contains(row, "Attention Is All") {
		t.Errorf("the title is shown again: %q", row)
	}
	if strings.Contains(row, "Vaswani") {
		t.Errorf("the author is shown again: %q", row)
	}
}

// Tags sit flush right, so they form a column the eye can scan.
func TestListRowTagsAreRightAligned(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m.lib.Store.Set("a.pdf", &meta.Entry{Title: "T", Tags: []string{"x"}})
	_ = m.lib.Rescan()
	m.rebuild()
	row := stripEsc(m.listRow(0, 40))
	if len([]rune(row)) != 40 {
		t.Fatalf("row is %d cells, want 40: %q", len([]rune(row)), row)
	}
	// The tag ends one cell before the right edge.
	if !strings.HasSuffix(row, "x ") {
		t.Errorf("tag is not flush right: %q", row)
	}
}

// A paper with no tags still gets a full-width row.
func TestListRowWithoutTags(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m.rebuild()
	row := stripEsc(m.listRow(0, 30))
	if len([]rune(row)) != 30 {
		t.Errorf("row is %d cells, want 30: %q", len([]rune(row)), row)
	}
	if !strings.Contains(row, "a.pdf") {
		t.Errorf("name missing: %q", row)
	}
}

// In a narrow pane the name wins and the tags are cut, because the name is what
// identifies the row.
func TestListRowNarrowKeepsName(t *testing.T) {
	m := newTestModel(t, "a-long-file-name.pdf")
	m.lib.Store.Set("a-long-file-name.pdf", &meta.Entry{
		Title: "T", Tags: []string{"verylongtagname", "another"},
	})
	_ = m.lib.Rescan()
	m.rebuild()
	row := stripEsc(m.listRow(0, 24))
	if len([]rune(row)) != 24 {
		t.Errorf("row is %d cells: %q", len([]rune(row)), row)
	}
	if !strings.Contains(row, "a-long") {
		t.Errorf("the name was dropped instead of the tags: %q", row)
	}
}

// The whole view keeps one line per paper regardless of metadata length.
func TestListViewIsOneLinePerPaper(t *testing.T) {
	m := newTestModel(t, "a.pdf", "b.pdf")
	m.lib.Store.Set("a.pdf", &meta.Entry{
		Title: strings.Repeat("long title ", 20), Author: "someone",
		Tags: []string{"t1", "t2", "t3"},
	})
	_ = m.lib.Rescan()
	m.rebuild()
	before := len(strings.Split(stripEsc(m.View()), "\n"))
	m = key(m, "j")
	after := len(strings.Split(stripEsc(m.View()), "\n"))
	if before != after {
		t.Errorf("view height changed from %d to %d", before, after)
	}
}
