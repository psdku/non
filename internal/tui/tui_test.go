package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"non/internal/app"
	"non/internal/meta"
)

// newTestModel builds a model over a temp directory with stub PDFs.
func newTestModel(t *testing.T, names ...string) *Model {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("%PDF-1.4\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	// Without a TTY lipgloss falls back to the Ascii profile and drops every
	// style, which would hide the reverse-video cursor from the tests. Pin a
	// colour profile so the rendered output is what a terminal would show.
	lipgloss.SetColorProfile(termenv.ANSI256)
	// Give the model a deterministic size.
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return mm.(*Model)
}

// key sends a key press and returns the model.
func key(m *Model, s string) *Model {
	var msg tea.KeyMsg
	switch s {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "ctrl+n":
		msg = tea.KeyMsg{Type: tea.KeyCtrlN}
	case "ctrl+p":
		msg = tea.KeyMsg{Type: tea.KeyCtrlP}
	case "backspace":
		msg = tea.KeyMsg{Type: tea.KeyBackspace}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
	mm, _ := m.Update(msg)
	return mm.(*Model)
}

// typeString sends every rune of s to the model.
func typeString(m *Model, s string) *Model {
	for _, r := range s {
		m = key(m, string(r))
	}
	return m
}

func TestNavigation(t *testing.T) {
	m := newTestModel(t, "a.pdf", "b.pdf", "c.pdf")
	if got := m.current().Name; got != "a.pdf" {
		t.Fatalf("initial selection = %q", got)
	}
	m = key(m, "j")
	if got := m.current().Name; got != "b.pdf" {
		t.Errorf("after j = %q", got)
	}
	m = key(m, "down")
	if got := m.current().Name; got != "c.pdf" {
		t.Errorf("after down = %q", got)
	}
	// Clamped at the end.
	m = key(m, "j")
	if got := m.current().Name; got != "c.pdf" {
		t.Errorf("after j at end = %q", got)
	}
	m = key(m, "k")
	if got := m.current().Name; got != "b.pdf" {
		t.Errorf("after k = %q", got)
	}
	m = key(m, "ctrl+p")
	if got := m.current().Name; got != "a.pdf" {
		t.Errorf("after ctrl+p = %q", got)
	}
	m = key(m, "ctrl+p")
	if got := m.current().Name; got != "a.pdf" {
		t.Errorf("after ctrl+p at start = %q", got)
	}
	// Home and end.
	m = key(m, "G")
	if got := m.current().Name; got != "c.pdf" {
		t.Errorf("after G = %q", got)
	}
	m = key(m, "g")
	if got := m.current().Name; got != "a.pdf" {
		t.Errorf("after g = %q", got)
	}
}

func TestSearchEnterKeepsQuery(t *testing.T) {
	m := newTestModel(t, "attention.pdf", "gpt3.pdf", "vllm.pdf")
	m = key(m, "/")
	if m.mode != modeSearch {
		t.Fatalf("mode = %v, want search", m.mode)
	}
	m = typeString(m, "gpt")
	if got := len(m.visible); got != 1 {
		t.Fatalf("visible = %d (%v), want 1", got, m.visibleNames())
	}
	if got := m.current().Name; got != "gpt3.pdf" {
		t.Errorf("selection = %q", got)
	}
	// Enter leaves search mode and keeps the query and the filtered list.
	m = key(m, "enter")
	if m.mode != modeBrowse {
		t.Errorf("mode = %v, want browse", m.mode)
	}
	if m.quat != "gpt" {
		t.Errorf("query = %q, want it retained", m.quat)
	}
	if got := len(m.visible); got != 1 {
		t.Errorf("visible after enter = %d, want 1", got)
	}
	// Pressing / again prefills the previous query.
	m = key(m, "/")
	if m.search.Value() != "gpt" {
		t.Errorf("query not prefilled: %q", m.search.Value())
	}
	// Escape inside search clears it, and the full list comes back.
	m = key(m, "esc")
	if m.quat != "" {
		t.Errorf("query not cleared: %q", m.quat)
	}
	if got := len(m.visible); got != 3 {
		t.Errorf("visible after clearing = %d, want 3", got)
	}
	// A query retained by enter is cleared by escape in browse mode too.
	m = key(m, "/")
	m = typeString(m, "gpt")
	m = key(m, "enter")
	if len(m.visible) != 1 {
		t.Fatalf("visible = %d, want 1", len(m.visible))
	}
	m = key(m, "esc")
	if len(m.visible) != 3 {
		t.Errorf("visible after browse esc = %d, want 3", len(m.visible))
	}
}

func TestViewRenders(t *testing.T) {
	m := newTestModel(t, "attention.pdf", "gpt3.pdf")
	m.lib.Store.Config.SetTag(-1, meta.Tag{Name: "transformer"})
	m.lib.Store.Set("attention.pdf", &meta.Entry{
		Title: "Attention Is All You Need", Author: "Vaswani", Tags: []string{"transformer"},
		Abstract: "The dominant sequence transduction models.",
	})
	m.lib.Rescan()
	m.rebuild()

	out := m.View()
	if !strings.Contains(out, "attention.pdf") {
		t.Errorf("view does not list files:\n%s", out)
	}
	if !strings.Contains(out, "Attention Is All You Need") {
		t.Errorf("view does not show the title:\n%s", out)
	}
	if !strings.Contains(out, "transformer") {
		t.Errorf("view does not show tags:\n%s", out)
	}
	// Every line must fit in the terminal width.
	for i, line := range strings.Split(out, "\n") {
		if w := len([]rune(stripAnsi(line))); w > 100 {
			t.Errorf("line %d is %d cells wide: %q", i, w, line)
		}
	}
	// And the view must fit the height.
	if n := len(strings.Split(out, "\n")); n > 30 {
		t.Errorf("view is %d lines tall, terminal is 30", n)
	}
}

func TestViewEmptyDirectory(t *testing.T) {
	m := newTestModel(t)
	out := m.View()
	if !strings.Contains(out, "no *.pdf files here") {
		t.Errorf("empty state not shown:\n%s", out)
	}
}

func TestViewSearchMode(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m = key(m, "/")
	out := m.View()
	if !strings.Contains(out, "search") && !strings.Contains(out, "/") {
		t.Errorf("search prompt missing:\n%s", out)
	}
}

func TestHelpScreen(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m = key(m, "?")
	out := m.View()
	for _, want := range []string{"rename", "search", "quit"} {
		if !strings.Contains(out, want) {
			t.Errorf("help is missing %q:\n%s", want, out)
		}
	}
	m = key(m, "esc")
	if m.mode != modeBrowse {
		t.Errorf("mode = %v after esc", m.mode)
	}
}

func TestQuit(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	mm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if !mm.(*Model).quitting {
		t.Error("q did not set quitting")
	}
	if cmd == nil {
		t.Error("q did not return a command")
	}
}

func TestSmallTerminal(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 12, Height: 4})
	out := mm.(*Model).View()
	if out == "" {
		t.Error("empty view on a tiny terminal")
	}
}

func TestDetailsScroll(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	m.lib.Store.Set("a.pdf", &meta.Entry{Title: "T", Abstract: strings.Repeat("word ", 400)})
	m.lib.Rescan()
	m.rebuild()
	first := m.View()
	m = key(m, "J")
	if m.details != 1 {
		t.Errorf("details = %d", m.details)
	}
	if second := m.View(); second == first {
		t.Error("scrolling did not change the view")
	}
	m = key(m, "K")
	if m.details != 0 {
		t.Errorf("details = %d after K", m.details)
	}
	m = key(m, "K")
	if m.details != 0 {
		t.Errorf("details went negative: %d", m.details)
	}
}

func TestEditorRoundTrip(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	// The tags the editor writes must exist in the table, or the save is
	// refused (see applyEditor).
	m.lib.Store.Config.SetTag(-1, meta.Tag{Name: "one"})
	m.lib.Store.Config.SetTag(-1, meta.Tag{Name: "two"})
	// Simulate what $EDITOR would leave behind.
	path := filepath.Join(t.TempDir(), "meta.md")
	src := "# Edited Title\n- Author: Someone\n- Tags: one, two\n\n# Comment\nhello\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	mm, _ := m.Update(editorDoneMsg{name: "a.pdf", path: path})
	m = mm.(*Model)
	e, ok := m.lib.Store.Lookup("a.pdf")
	if !ok {
		t.Fatal("metadata not stored")
	}
	if e.Title != "Edited Title" || e.Author != "Someone" || e.Comment != "hello" {
		t.Errorf("entry = %+v", e)
	}
	if strings.Join(e.Tags, ",") != "one,two" {
		t.Errorf("tags = %v", e.Tags)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("temporary metadata file was not removed")
	}
}

func TestEditorRejectsMissingTitle(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	before, _ := m.lib.EnsureMeta(m.current(), false)
	oldTitle := before.Title

	path := filepath.Join(t.TempDir(), "meta.md")
	if err := os.WriteFile(path, []byte("- Author: nobody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mm, _ := m.Update(editorDoneMsg{name: "a.pdf", path: path})
	m = mm.(*Model)
	if !m.statusK {
		t.Errorf("expected an error, got %q", m.status)
	}
	e, _ := m.lib.Store.Lookup("a.pdf")
	if e.Title != oldTitle {
		t.Errorf("title changed to %q despite the error", e.Title)
	}
}

func TestSplitCommand(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"firefox", []string{"firefox"}},
		{"firefox --new-tab", []string{"firefox", "--new-tab"}},
		{`open -a "My App"`, []string{"open", "-a", "My App"}},
		{"  spaced   out  ", []string{"spaced", "out"}},
		{"", nil},
		{`a 'b c' d`, []string{"a", "b c", "d"}},
	}
	for _, c := range cases {
		got := splitCommand(c.in)
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("splitCommand(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLaunchMissingProgram(t *testing.T) {
	if err := Launch("definitely-not-a-real-program-xyz", "/tmp/x.pdf"); err == nil {
		t.Error("launching a missing program should fail")
	}
}

func TestDefaultTools(t *testing.T) {
	tools := meta.DefaultTools()
	if len(tools) == 0 {
		t.Fatal("no platform default tool")
	}
	if tools[0].Command == "" {
		t.Error("default tool has no command")
	}
	if tools[0].Type != meta.ToolGUI {
		t.Errorf("default tool type = %q", tools[0].Type)
	}
}

func TestAddToolFlag(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	if err := m.AddTool("zathura=zathura --fork,tui"); err != nil {
		t.Fatal(err)
	}
	tool, ok := m.lib.Store.Config.Tool("zathura")
	if !ok {
		t.Fatal("tool not added")
	}
	if tool.Command != "zathura --fork" || !tool.IsTUI() {
		t.Errorf("tool = %+v", tool)
	}
	// The tool is persisted, so the next run uses it.
	again, err := meta.Load(m.dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := again.Config.Tool("zathura"); !ok {
		t.Error("tool not saved")
	}
	// A malformed spec is refused rather than half applied.
	if err := m.AddTool("broken"); err == nil {
		t.Error("expected an error for a spec without =")
	}
	if err := m.AddTool("x="); err == nil {
		t.Error("expected an error for a spec without a command")
	}
}

func TestStatusLine(t *testing.T) {
	m := newTestModel(t, "a.pdf", "b.pdf")
	if got := m.statusLine(); got != "2 papers" {
		t.Errorf("statusLine = %q", got)
	}
	m.quat = "a"
	m.metaUI = false
	m.rebuild()
	if got := m.statusLine(); got != "1 of 2 papers" {
		t.Errorf("statusLine = %q", got)
	}
}

func TestWrapText(t *testing.T) {
	got := wrapText("one two three four", 8)
	if strings.Join(got, "|") != "one two|three|four" {
		t.Errorf("wrapText = %q", got)
	}
	if got := wrapText("", 10); len(got) != 1 || got[0] != "" {
		t.Errorf("wrapText of empty = %q", got)
	}
}

var _ = app.Paper{}

func TestReextractKeepsManualEdits(t *testing.T) {
	m := newTestModel(t, "a.pdf")
	if err := m.lib.SaveMeta("a.pdf", &meta.Entry{
		Title: "Mine", Author: "Me", Tags: []string{"keep"}, Comment: "note",
	}); err != nil {
		t.Fatal(err)
	}
	if err := m.lib.Rescan(); err != nil {
		t.Fatal(err)
	}
	m.rebuild()
	cmd := m.reextractCmd()
	if cmd == nil {
		t.Fatal("no command")
	}
	mm, _ := m.Update(cmd())
	m = mm.(*Model)
	e, _ := m.lib.Store.Lookup("a.pdf")
	if e.Title != "Mine" || e.Author != "Me" || e.Comment != "note" {
		t.Errorf("manual fields lost: %+v", e)
	}
	if strings.Join(e.Tags, ",") != "keep" {
		t.Errorf("tags lost: %v", e.Tags)
	}
}

func TestReextractFillsAutomaticEntry(t *testing.T) {
	m := newTestModel(t, "stub.pdf")
	// An automatic entry that lost its title gets it back.
	if err := m.lib.SaveMeta("stub.pdf", &meta.Entry{Title: "", Auto: true}); err != nil {
		t.Fatal(err)
	}
	if err := m.lib.Rescan(); err != nil {
		t.Fatal(err)
	}
	m.rebuild()
	mm, _ := m.Update(m.reextractCmd()())
	m = mm.(*Model)
	e, _ := m.lib.Store.Lookup("stub.pdf")
	if e.Title != "stub" {
		t.Errorf("Title = %q, want the file name fallback", e.Title)
	}
}

// The TUI must see the tools and tags inherited from the user configuration:
// reopening the library from a path instead of using the loaded one silently
// dropped them.
func TestNewInheritsGlobalConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.pdf"), []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := &meta.GlobalConfig{
		Tools:    []meta.Tool{{Name: "zathura", Command: "zathura", Type: meta.ToolGUI}},
		TagTable: []meta.Tag{{Name: "inherited", Description: "from the user config"}},
	}
	lib, err := app.OpenDirWith(dir, g)
	if err != nil {
		t.Fatal(err)
	}
	m, err := New(lib)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.defaultTool(); !ok {
		t.Error("the inherited tool is missing")
	}
	if _, ok := m.lib.Store.Config.Tag("inherited"); !ok {
		t.Error("the inherited tag table is missing")
	}
	// And the settings modal shows them, rather than an empty list.
	m.openSettings()
	m.settings.page = pageTags
	m.refreshTags()
	if len(m.settings.tags) != 1 {
		t.Errorf("settings shows %d tags, want the inherited one", len(m.settings.tags))
	}
}
