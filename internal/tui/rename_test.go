package tui

import (
	"strings"
	"testing"
)

// A name that needs cleaning opens a dialog describing the change instead of
// renaming silently; declining leaves the file alone.
func TestRenameAsksBeforeNormalizing(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m = key(m, "r")
	m.input.SetValue(`my "paper" name`)
	m = key(m, "enter")

	if m.mode != modeConfirm {
		t.Fatalf("mode = %v, want the confirm dialog", m.mode)
	}
	if m.confirm == nil {
		t.Fatal("no dialog state")
	}
	if !strings.Contains(m.confirm.Warning(), "quotes") {
		t.Errorf("dialog does not explain the change: %q", m.confirm.Warning())
	}
	// The view shows it over the browser.
	out := stripEsc(m.View())
	if !strings.Contains(out, "removed quotes") {
		t.Errorf("dialog not rendered:\n%s", out)
	}
	if !strings.Contains(out, "a.pdf") {
		t.Errorf("browser not visible behind the dialog:\n%s", out)
	}

	// Decline: nothing happens.
	m = key(m, "n")
	if m.mode != modeBrowse || m.confirm != nil {
		t.Errorf("mode = %v after declining", m.mode)
	}
	if len(m.lib.Papers) != 1 || m.lib.Papers[0].Name != "a.pdf" {
		t.Errorf("papers = %v, want the original file", names(m))
	}
}

func TestRenameAcceptsNormalizedName(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m = key(m, "r")
	m.input.SetValue(`my "paper"`)
	m = key(m, "enter")
	m = key(m, "y") // accept
	if m.mode != modeBrowse {
		t.Fatalf("mode = %v", m.mode)
	}
	if got := names(m); len(got) != 1 || got[0] != "my-paper.pdf" {
		t.Errorf("papers = %v, want my-paper.pdf", got)
	}
	if !strings.Contains(m.status, "my-paper.pdf") {
		t.Errorf("status = %q", m.status)
	}
}

// A clean name renames immediately with no dialog.
func TestRenameCleanNameSkipsDialog(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m = key(m, "r")
	m.input.SetValue("clean-name.pdf")
	m = key(m, "enter")
	if m.mode != modeBrowse {
		t.Errorf("mode = %v, want browse with no dialog", m.mode)
	}
	if got := names(m); got[0] != "clean-name.pdf" {
		t.Errorf("papers = %v", got)
	}
}

func TestRenameDialogKeys(t *testing.T) {
	for _, k := range []string{"esc", "n", "q"} {
		m := newTestModel(t, "a.pdf")
		m = key(m, "r")
		m.input.SetValue("a\nb")
		m = key(m, "enter")
		m = key(m, k)
		if m.mode != modeBrowse || m.confirm != nil {
			t.Errorf("%q did not decline: mode=%v", k, m.mode)
		}
		if got := names(m); got[0] != "a.pdf" {
			t.Errorf("%q renamed anyway: %v", k, got)
		}
	}
	for _, k := range []string{"y", "enter"} {
		m := newTestModel(t, "a.pdf")
		m = key(m, "r")
		m.input.SetValue("a\nb")
		m = key(m, "enter")
		m = key(m, k)
		if got := names(m); got[0] != "a-b.pdf" {
			t.Errorf("%q did not accept: %v", k, got)
		}
	}
}

// Tab moves between the buttons, and the focused one decides what enter does.
func TestRenameDialogButtonFocus(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m = key(m, "r")
	m.input.SetValue("x\ny")
	m = key(m, "enter")
	if !m.confirm.yes {
		t.Fatal("the affirmative button should be focused first")
	}
	m = key(m, "tab")
	if m.confirm.yes {
		t.Fatal("tab should move focus to cancel")
	}
	m = key(m, "enter") // cancel is focused
	if m.confirm != nil {
		t.Error("dialog still open")
	}
	if got := names(m); got[0] != "a.pdf" {
		t.Errorf("enter on cancel renamed: %v", got)
	}
}

// An empty or unchanged name closes the prompt without touching the file.
func TestRenameNoChange(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m = key(m, "r")
	m = key(m, "enter")
	if m.mode != modeBrowse || m.confirm != nil {
		t.Errorf("mode = %v", m.mode)
	}
	if got := names(m); got[0] != "a.pdf" {
		t.Errorf("papers = %v", got)
	}
}
