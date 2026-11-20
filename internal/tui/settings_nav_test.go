package tui

import (
	"testing"

	"non/internal/meta"
)

// nav opens settings, walks to a row by label and returns the model.
func navTo(t *testing.T, m *Model, label string) *Model {
	t.Helper()
	m = key(m, "s")
	for i := 0; i < len(m.settings.root); i++ {
		if m.settings.root[i].label == label {
			m.settings.cursor = i
			return m
		}
	}
	t.Fatalf("no settings row %q; rows: %v", label, labels(m.settings.root))
	return m
}

func labels(fields []setField) []string {
	out := make([]string, len(fields))
	for i, f := range fields {
		out[i] = f.label
	}
	return out
}

// The reported crash: open the tools list, press esc, and the settings page was
// empty because leaving the tool form cleared the shared field slice. Entering
// it then indexed an empty list.
func TestSettingsRootSurvivesSubPageRoundTrip(t *testing.T) {
	for _, label := range []string{"Manage tools", "Manage tags"} {
		for _, viaForm := range []bool{false, true} {
			m := navTo(t, newTestModel(t, "a.pdf"), label)
			m = key(m, "enter") // into the list
			if viaForm {
				m = key(m, "a") // into the add form
				m = key(m, "esc")
			}
			m = key(m, "esc") // back to the root page
			if m.settings.page != pageRoot {
				t.Fatalf("%s: page = %v, want the root page", label, m.settings.page)
			}
			if got := len(m.settings.root); got == 0 {
				t.Fatalf("%s: the settings page lost its rows", label)
			}
			// Every key on the root page must be safe. Enter re-opens the list
			// the cursor is on, so leave that for last.
			for _, k := range []string{"h", "l", "j", "k"} {
				m = key(m, k)
				if m.settings == nil {
					t.Fatalf("%s: modal closed on %q", label, k)
				}
				if m.settings.page != pageRoot {
					t.Fatalf("%s: page changed to %v on %q", label, m.settings.page, k)
				}
			}
			// Toggling still works on the root page.
			m.settings.cursor = 0
			before := m.lib.Store.Config.UseEditor
			m = key(m, " ")
			if m.lib.Store.Config.UseEditor == before {
				t.Errorf("%s: space no longer toggles a setting", label)
			}
			// And enter on a "Manage …" row still opens its page.
			m.settings.cursor = rootRow(m.settings.root, label)
			m = key(m, "enter")
			if m.settings.page == pageRoot {
				t.Errorf("%s: enter no longer opens the page", label)
			}
		}
	}
}

// The root rows must still be the real settings, not a stale copy.
func TestSettingsRootRowsIntactAfterToolEdit(t *testing.T) {
	m := navTo(t, newTestModel(t, "a.pdf"), "Manage tools")
	m = key(m, "enter")
	m = key(m, "a")
	m.settings.form[0].input.SetValue("x")
	m.settings.form[1].input.SetValue("x")
	m = key(m, "ctrl+s") // saves and returns to the tools list
	if m.settings.page != pageTools {
		t.Fatalf("page = %v", m.settings.page)
	}
	m = key(m, "esc")
	got := labels(m.settings.root)
	want := []string{"Open editor", "Search metadata", "Default tool", "Manage tools", "Manage tags"}
	if len(got) != len(want) {
		t.Fatalf("root rows = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("root row %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// A stale cursor left pointing past the end of a shrinking list must not panic.
func TestSettingsStaleCursorIsClamped(t *testing.T) {
	m := navTo(t, newTestModel(t, "a.pdf"), "Manage tools")
	m = key(m, "enter")
	_ = m.lib.Store.Config.AddTool(meta.Tool{Name: "a", Command: "a", Type: meta.ToolGUI})
	m.refreshTools()
	m.settings.cursor = 5 // beyond the end
	m = key(m, "esc")
	m.settings.cursor = 99
	for _, k := range []string{"enter", " ", "j", "k", "l", "h"} {
		m = key(m, k)
	}
	if m.settings != nil && m.settings.page == pageTools {
		m = key(m, "esc")
	}
	// And the form pages clamp too.
	m.settings.cursor = 99
	m = key(m, "enter")
	m.settings.cursor = 99
	m = key(m, "esc")
	if m.settings == nil {
		t.Fatal("the modal closed on a stale cursor")
	}
}
