package tui

import (
	"strings"
	"testing"

	"non/internal/meta"
)

// --- settings -------------------------------------------------------------

func TestSettingsOpensAndCloses(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m = key(m, "s")
	if m.mode != modeSettings {
		t.Fatalf("mode = %v, want settings", m.mode)
	}
	if m.settings == nil || m.settings.page != pageRoot {
		t.Fatalf("settings = %+v", m.settings)
	}
	// Every root row must be navigable and rendered.
	for i := 0; i < len(m.settings.root); i++ {
		m = key(m, "j")
	}
	if m.settings.cursor != 0 {
		t.Errorf("cursor wraps to 0, got %d", m.settings.cursor)
	}
	m = key(m, "esc")
	if m.mode != modeBrowse {
		t.Errorf("mode after esc = %v", m.mode)
	}
	if m.settings != nil {
		t.Error("settings state kept after closing")
	}
}

func TestSettingsTogglesEditingMode(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m = key(m, "s")
	if m.settings.root[0].label != "Open editor" {
		t.Fatalf("first row = %q", m.settings.root[0].label)
	}
	if m.settings.root[0].on {
		t.Fatal("the editor should be off by default")
	}
	m = key(m, " ")
	if !m.lib.Store.Config.UseEditor {
		t.Error("turning the editor on should set use_editor")
	}
	// The choice is persisted, not just held in memory.
	again, err := meta.Load(m.dir)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Config.UseEditor {
		t.Error("setting not saved")
	}
	m = key(m, " ")
	if m.lib.Store.Config.UseEditor {
		t.Error("turning the editor off should go back to the built-in form")
	}
}

func TestSettingsSearchMetaToggle(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m = key(m, "s")
	m = key(m, "j") // search metadata
	m = key(m, " ")
	if !m.metaUI {
		t.Error("the toggle should apply to the current session too")
	}
	if !m.lib.Store.Config.SearchMeta {
		t.Error("search_meta not stored")
	}
}

func TestSettingsToolsAddEditDelete(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m = key(m, "s")
	m.settings.cursor = 3
	m = key(m, "enter")
	if m.settings.page != pageTools {
		t.Fatalf("page = %v", m.settings.page)
	}
	m = key(m, "a")
	if m.settings.page != pageToolForm {
		t.Fatalf("page = %v, want the tool form", m.settings.page)
	}
	// Name must be typed; an empty name is refused.
	m = key(m, "enter")
	m = key(m, "enter")
	m = key(m, "enter")
	if m.settings.page != pageToolForm || !m.settings.statusK {
		t.Errorf("empty tool accepted: page=%v status=%q", m.settings.page, m.settings.status)
	}

	m.settings.form[0].input.SetValue("zathura")
	m.settings.form[1].input.SetValue("zathura --fork")
	m.settings.form[2].input.SetValue("tui")
	m = key(m, "ctrl+s")

	if m.settings.page != pageTools {
		t.Fatalf("page after save = %v", m.settings.page)
	}
	tool, ok := m.lib.Store.Config.Tool("zathura")
	if !ok {
		t.Fatal("tool not saved")
	}
	if tool.Command != "zathura --fork" || !tool.IsTUI() {
		t.Errorf("tool = %+v", tool)
	}
	if m.lib.Store.Config.DefaultTool != "zathura" {
		t.Errorf("default = %q", m.lib.Store.Config.DefaultTool)
	}

	// Edit it.
	m = key(m, "enter")
	if m.settings.page != pageToolForm {
		t.Fatalf("page = %v", m.settings.page)
	}
	if m.settings.form[1].input.Value() != "zathura --fork" {
		t.Errorf("command not prefilled: %q", m.settings.form[1].input.Value())
	}
	m.settings.form[1].input.SetValue("zathura")
	m = key(m, "ctrl+s")
	if tool, _ := m.lib.Store.Config.Tool("zathura"); tool.Command != "zathura" {
		t.Errorf("edit not saved: %+v", tool)
	}

	// Delete it.
	m = key(m, "d")
	if _, ok := m.lib.Store.Config.Tool("zathura"); ok {
		t.Error("tool not deleted")
	}
}

func TestSettingsToolRenameKeepsDefault(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	_ = m.lib.Store.Config.AddTool(meta.Tool{Name: "old", Command: "old", Type: meta.ToolGUI})
	m.lib.Store.Config.DefaultTool = "old"
	m = key(m, "s")
	m.settings.cursor = 3
	m = key(m, "enter")
	m = key(m, "enter")
	m.settings.form[0].input.SetValue("new")
	m = key(m, "ctrl+s")
	if _, ok := m.lib.Store.Config.Tool("old"); ok {
		t.Error("old name still present")
	}
	if _, ok := m.lib.Store.Config.Tool("new"); !ok {
		t.Fatal("renamed tool missing")
	}
	if m.lib.Store.Config.DefaultTool != "new" {
		t.Errorf("default = %q, want new", m.lib.Store.Config.DefaultTool)
	}
}

func TestSettingsTagsAddRenameDelete(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m.lib.Store.Set("a.pdf", &meta.Entry{Title: "A", Tags: []string{"kvcache"}})
	_ = m.lib.Rescan()

	m = key(m, "s")
	m.settings.cursor = 4
	m = key(m, "enter")
	if m.settings.page != pageTags {
		t.Fatalf("page = %v", m.settings.page)
	}
	m = key(m, "a")
	m.settings.form[0].input.SetValue("kvcache")
	m.settings.form[1].input.SetValue("KV cache work")
	m = key(m, "ctrl+s")
	if got := m.lib.Store.Config.DescribeTag("kvcache"); got != "KV cache work" {
		t.Errorf("description = %q", got)
	}

	// Renaming the tag follows through to the paper.
	m = key(m, "enter")
	m.settings.form[0].input.SetValue("kv_cache")
	m = key(m, "ctrl+s")
	e, _ := m.lib.Store.Lookup("a.pdf")
	if len(e.Tags) != 1 || e.Tags[0] != "kv_cache" {
		t.Errorf("entry tags = %v", e.Tags)
	}

	// Deleting the tag strips it from the paper too.
	m = key(m, "d")
	e, _ = m.lib.Store.Lookup("a.pdf")
	if len(e.Tags) != 0 {
		t.Errorf("tags after delete = %v", e.Tags)
	}
}

// A tag name may hold letters, digits, spaces and _ - & ( ) — not reserved punctuation.
func TestSettingsTagNameRules(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m = key(m, "s")
	m.settings.cursor = 4
	m = key(m, "enter")
	m = key(m, "a")
	for _, bad := range []string{"kv|cache", `kv"cache`, "kv&&cache", "kv!cache", ""} {
		m.settings.form[0].input.SetValue(bad)
		m = key(m, "ctrl+s")
		if m.settings.page != pageTagForm {
			t.Fatalf("%q was accepted as a tag name", bad)
		}
		if !m.settings.statusK {
			t.Errorf("%q: no error shown, status = %q", bad, m.settings.status)
		}
	}
	// A good name goes through, including the characters people actually use.
	m.settings.form[0].input.SetValue("KV-cache-paged")
	m = key(m, "ctrl+s")
	if m.settings.page != pageTags {
		t.Fatalf("\"KV-cache-(paged)\" was refused: %q", m.settings.status)
	}
	if _, ok := m.lib.Store.Config.Tag("KV-cache-paged"); !ok {
		t.Error("tag not stored")
	}
}

func TestSettingsTagRejectsDuplicate(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m.lib.Store.Config.SetTag(-1, meta.Tag{Name: "a"})
	m.lib.Store.Config.SetTag(-1, meta.Tag{Name: "b"})
	m = key(m, "s")
	m.settings.cursor = 4
	m = key(m, "enter")
	m = key(m, "a")
	m.settings.root[0].input.SetValue("A")
	m = key(m, "ctrl+s")
	if !m.settings.statusK {
		t.Error("duplicate tag accepted (case-insensitively)")
	}
	if m.settings.page != pageTagForm {
		t.Error("form closed on an error")
	}
}

func TestSettingsViewRenders(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	for _, size := range [][2]int{{100, 30}, {60, 20}, {40, 14}, {120, 40}} {
		m.width, m.height = size[0], size[1]
		m.mode = modeBrowse
		m = key(m, "s")
		out := m.View()
		if !strings.Contains(out, "settings") {
			t.Errorf("%dx%d: settings not rendered", size[0], size[1])
		}
		if got := len(strings.Split(out, "\n")); got != m.height {
			t.Errorf("%dx%d: rendered %d lines, want %d", size[0], size[1], got, m.height)
		}
		for _, page := range []setPage{pageRoot, pageTools, pageTags} {
			m.settings.page = page
			if page == pageTools {
				_ = m.lib.Store.Config.AddTool(meta.Tool{Name: "t", Command: "t"})
				m.refreshTools()
			}
			if page == pageTags {
				m.lib.Store.Config.SetTag(-1, meta.Tag{Name: "tag", Description: "d"})
				m.refreshTags()
			}
			out := m.View()
			if strings.TrimSpace(out) == "" {
				t.Errorf("%dx%d page %v: empty view", size[0], size[1], page)
			}
			if got := len(strings.Split(out, "\n")); got != m.height {
				t.Errorf("%dx%d page %v: %d lines, want %d", size[0], size[1], page, got, m.height)
			}
		}
		m = key(m, "esc")
		m = key(m, "esc")
		m = key(m, "esc")
	}
}
