package meta

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigToolDefaults(t *testing.T) {
	var c Config
	if c.UseEditor {
		t.Error("$EDITOR should be off by default: the built-in form is used")
	}
	tools := c.EffectiveTools()
	if len(tools) == 0 {
		t.Fatal("expected a platform default tool")
	}
	if tools[0].Type != ToolGUI {
		t.Errorf("default tool type = %q", tools[0].Type)
	}
	def, ok := c.EffectiveDefault()
	if !ok || def.Name != tools[0].Name {
		t.Errorf("default = %+v, %v", def, ok)
	}
}

func TestConfigToolRoundTrip(t *testing.T) {
	var c Config
	if err := c.AddTool(Tool{Name: "zathura", Command: "zathura --fork", Type: "TUI"}); err != nil {
		t.Fatal(err)
	}
	if err := c.AddTool(Tool{Name: "firefox", Command: "firefox", Type: "gui"}); err != nil {
		t.Fatal(err)
	}
	if c.Tools[0].Type != ToolTUI {
		t.Errorf("type not normalised: %q", c.Tools[0].Type)
	}
	if c.DefaultTool != "zathura" {
		t.Errorf("default = %q, want the first tool", c.DefaultTool)
	}

	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	var back Config
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Tools) != 2 || back.Tools[0].Command != "zathura --fork" {
		t.Errorf("tools lost: %+v", back.Tools)
	}
	if back.DefaultTool != "zathura" {
		t.Errorf("default lost: %q", back.DefaultTool)
	}
}

func TestConfigToolValidation(t *testing.T) {
	var c Config
	if err := c.AddTool(Tool{Command: "x"}); err == nil {
		t.Error("expected an error for a tool without a name")
	}
	if err := c.AddTool(Tool{Name: "x"}); err == nil {
		t.Error("expected an error for a tool without a command")
	}
	if err := c.AddTool(Tool{Name: "x", Command: "y"}); err != nil {
		t.Fatal(err)
	}
	// Adding the same name again replaces it rather than duplicating.
	if err := c.AddTool(Tool{Name: "x", Command: "z", Type: ToolTUI}); err != nil {
		t.Fatal(err)
	}
	if len(c.Tools) != 1 || c.Tools[0].Command != "z" {
		t.Errorf("tools = %+v", c.Tools)
	}
}

func TestConfigRemoveToolFixesDefault(t *testing.T) {
	var c Config
	_ = c.AddTool(Tool{Name: "a", Command: "a"})
	_ = c.AddTool(Tool{Name: "b", Command: "b"})
	c.DefaultTool = "a"
	c.RemoveTool("a")
	if len(c.Tools) != 1 || c.Tools[0].Name != "b" {
		t.Fatalf("tools = %+v", c.Tools)
	}
	if c.DefaultTool != "b" {
		t.Errorf("default = %q, want b", c.DefaultTool)
	}
	c.RemoveTool("b")
	if ok := c.EffectiveTools(); len(ok) == 0 {
		t.Error("removing every tool should fall back to the platform default")
	}
}

func TestConfigLegacyOpenerMigrated(t *testing.T) {
	// The first format kept a single "opener" string in a flat config map.
	raw := `{"version":1,"config":{"opener":"firefox --new-tab","other":"keep"},"entries":{}}`
	var s Store
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		t.Fatal(err)
	}
	if len(s.Config.Tools) != 1 {
		t.Fatalf("tools = %+v", s.Config.Tools)
	}
	tool := s.Config.Tools[0]
	if tool.Command != "firefox --new-tab" || tool.Name != "firefox" || tool.Type != ToolGUI {
		t.Errorf("migrated tool = %+v", tool)
	}
	if s.Config.DefaultTool != "firefox" {
		t.Errorf("default = %q", s.Config.DefaultTool)
	}
	// Unknown keys of the old map are preserved, not silently dropped.
	if string(s.Config.Fields["other"]) != `"keep"` {
		t.Errorf("unknown config key lost: %v", s.Config.Fields)
	}
	// The legacy key must not be written back.
	b, err := json.Marshal(s.Config)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "opener") {
		t.Errorf("legacy key written back: %s", b)
	}
}

func TestConfigOpenEditor(t *testing.T) {
	var c Config
	if err := json.Unmarshal([]byte(`{"open_editor":true}`), &c); err != nil {
		t.Fatal(err)
	}
	if !c.UseEditor {
		t.Error("open_editor:true should mean $EDITOR is used")
	}
	// The older inverted key is still understood.
	var old Config
	if err := json.Unmarshal([]byte(`{"inline_editor":false}`), &old); err != nil {
		t.Fatal(err)
	}
	if !old.UseEditor {
		t.Error("inline_editor:false means the old inline editor was off, i.e. $EDITOR")
	}
	b, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"open_editor":true`) {
		t.Errorf("marshal = %s", b)
	}
	if strings.Contains(string(b), "inline_editor") {
		t.Errorf("legacy key written back: %s", b)
	}
}

func TestTagTable(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Set("a.pdf", &Entry{Title: "A", Tags: []string{"kvcache", "serving"}})
	s.Set("b.pdf", &Entry{Title: "B", Tags: []string{"kvcache"}})
	name, added := s.Config.EnsureTag("kvcache")
	if !added || name != "kvcache" {
		t.Errorf("EnsureTag = %q, %v", name, added)
	}
	if _, added := s.Config.EnsureTag("KVCACHE"); added {
		t.Error("EnsureTag should be case-insensitive")
	}
	s.Config.SetTag(-1, Tag{Name: "serving", Description: "LLM serving"})

	if got := s.TagsInUse(); got["kvcache"] != 2 || got["serving"] != 1 {
		t.Errorf("usage = %v", got)
	}
	if got := s.Config.DescribeTag("serving"); got != "LLM serving" {
		t.Errorf("description = %q", got)
	}
}

func TestStoreRenameTagUpdatesEntries(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	s.Set("a.pdf", &Entry{Title: "A", Tags: []string{"kvcache", "serving"}})
	s.Set("b.pdf", &Entry{Title: "B", Tags: []string{"kvcache"}})
	s.Config.SetTag(-1, Tag{Name: "kvcache", Description: "KV cache"})

	s.RenameTag("kvcache", "kv-cache")

	for _, name := range []string{"a.pdf", "b.pdf"} {
		e, _ := s.Lookup(name)
		found := false
		for _, tag := range e.Tags {
			if tag == "kv-cache" {
				found = true
			}
			if tag == "kvcache" {
				t.Errorf("%s still has the old tag %q", name, tag)
			}
		}
		if !found {
			t.Errorf("%s: renamed tag missing: %v", name, e.Tags)
		}
	}
	if got := s.Config.DescribeTag("kv-cache"); got != "KV cache" {
		t.Errorf("description lost in rename: %q", got)
	}
	if _, ok := s.Config.Tag("kvcache"); ok {
		t.Error("old tag still in the table")
	}
}

func TestStoreDeleteTagRemovesFromEntries(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	s.Set("a.pdf", &Entry{Title: "A", Tags: []string{"kvcache", "serving"}})
	s.Config.SetTag(-1, Tag{Name: "kvcache"})
	s.Config.SetTag(-1, Tag{Name: "serving"})

	s.DeleteTag("kvcache")

	e, _ := s.Lookup("a.pdf")
	if len(e.Tags) != 1 || e.Tags[0] != "serving" {
		t.Errorf("tags = %v", e.Tags)
	}
	if _, ok := s.Config.Tag("kvcache"); ok {
		t.Error("tag still in the table")
	}
}

func TestTagJSONAcceptsBareString(t *testing.T) {
	// The very first tag table stored plain strings.
	var c Config
	raw := `{"tags":["kvcache",{"name":"serving","description":"batching"}]}`
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		t.Fatal(err)
	}
	if len(c.TagTable) != 2 {
		t.Fatalf("tags = %+v", c.TagTable)
	}
	if c.TagTable[0].Name != "kvcache" || c.TagTable[1].Description != "batching" {
		t.Errorf("tags = %+v", c.TagTable)
	}
}

func TestConfigUnknownKeysSurvive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, StoreFile)
	raw := `{"version":1,"config":{"open_editor":true,"future":{"a":1}},"entries":{}}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Config.UseEditor {
		t.Error("open_editor not read")
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"future"`) {
		t.Errorf("unknown config key dropped:\n%s", b)
	}
	if !strings.Contains(string(b), `"open_editor": true`) {
		t.Errorf("open_editor not written:\n%s", b)
	}
}

func TestStoreOmitsEmptyConfig(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	s.Set("a.pdf", &Entry{Title: "A"})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, StoreFile))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), `"config"`) {
		t.Errorf("empty config written:\n%s", b)
	}
}

func TestEntryDeleteField(t *testing.T) {
	e := &Entry{Title: "T"}
	e.SetField("DOI", "10.1/x")
	if !e.HasField("doi") {
		t.Fatal("SetField not case-insensitive")
	}
	e.DeleteField("doi")
	if e.HasField("doi") {
		t.Error("field not deleted")
	}
	if e.Fields != nil {
		t.Errorf("empty field map kept: %v", e.Fields)
	}
}
