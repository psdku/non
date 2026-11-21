package tui

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"non/internal/app"
	"non/internal/extract"
	"non/internal/filter"
	"non/internal/meta"
	"non/internal/search"
)

type mode int

const (
	modeBrowse mode = iota
	modeSearch
	modeActions
	modeOpener
	modeRename
	modeInspect
	modeHelp
	modeSettings
	modeMetaForm
	modeFilter
	modeConfirm
)

// action is one entry of the enter/dropdown menu.
type action struct {
	name string
	key  string
	run  func(*Model) tea.Cmd
}

// Model is the Bubble Tea model for non.
type Model struct {
	lib  *app.Library
	dir  string
	quat string // query for search

	// filter narrows the library by tags before the fuzzy query ranks what is
	// left: the two compose, they do not compete. filterText is what the user
	// typed, filterTree is the last expression that parsed, and filterErr is
	// the reason the current text could not be used (the old tree stays live
	// meanwhile, so the list does not flash empty mid-keystroke).
	filterText string
	filterTree filter.Node
	filterErr  error

	mode   mode
	search textinput.Model
	fin    textinput.Model // filter expression input
	input  textinput.Model // rename / open-with input
	metaUI bool            // search matches metadata too

	all     []*app.Paper
	visible []int // indices into all
	cursor  int   // index into visible
	offset  int   // first visible row in the list pane
	details int   // scroll offset of the details pane
	actions []action
	actCur  int

	inspect app.FileInfo
	status  string
	statusK bool // status is an error

	editorOverride string

	settings *settings // state of the settings modal when open
	metaForm *metaForm // state of the built-in metadata editor
	confirm  *confirm  // a yes/no dialog, when one is open

	openerList []meta.Tool // tools offered by the "open with" picker
	openerCur  int

	width, height int
	ready         bool
	quitting      bool

	// pendingEditor holds state across the $EDITOR round trip.
	pendingEditor struct {
		name string
		path string
	}
}

// Open builds a model for the given directory.
func Open(dir string) (*Model, error) {
	return OpenLibrary(dir, nil)
}

// OpenLibrary opens a directory, letting the user configuration fill in tools
// and tags the collection does not define.
//
// It takes the library rather than reopening it from a path, because main has
// already loaded it (with the global config) and reapplying flags to a second
// instance would drop exactly the inheritance this exists to provide.
func OpenLibrary(dir string, global *meta.GlobalConfig) (*Model, error) {
	lib, err := app.OpenDirWith(dir, global)
	if err != nil {
		return nil, err
	}
	return New(lib)
}

// New builds the model around an already loaded library.
func New(lib *app.Library) (*Model, error) {
	m := &Model{lib: lib, dir: lib.Dir}
	m.search = textinput.New()
	m.search.Placeholder = "fuzzy search…"
	m.search.Prompt = "/"
	m.search.CharLimit = 256
	m.input = textinput.New()
	m.input.CharLimit = 512
	m.fin = textinput.New()
	m.fin.Placeholder = "tag expression: LLM && !DB"
	m.fin.Prompt = "f> "
	m.fin.CharLimit = 512
	// The settings decide whether search looks inside metadata by default.
	m.metaUI = lib.Store.Config.SearchMeta
	m.rebuild()
	m.refreshStatus()
	// A store that another tool (or a hand edit) left with tags the table does
	// not know is a visible problem, not something to discover later: say so on
	// startup and leave it to the user to fix in the metadata form.
	if bad := lib.Store.BlockingTags(); len(bad) > 0 {
		m.setErr(fmt.Sprintf("%d unusable tag(s): %s — press m to fix, or add them in settings",
			len(bad), strings.Join(bad, ", ")))
	}
	return m, nil
}

// SetOpener installs a tool from --opener and makes it the default. It writes
// nothing: the caller saves the store, which keeps a single writer.
func (m *Model) SetOpener(s string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return
	}
	name := toolNameFromCommand(s)
	_ = m.lib.Store.Config.AddTool(meta.Tool{Name: name, Command: s, Type: meta.ToolGUI})
	m.lib.Store.Config.DefaultTool = name
}

// SetDefaultTool selects which configured tool plain "o" uses.
func (m *Model) SetDefaultTool(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	if _, ok := m.lib.Store.Config.Tool(name); ok {
		m.lib.Store.Config.DefaultTool = name
	}
}

// toolNameFromCommand derives a short display name from a command line.
func toolNameFromCommand(cmd string) string {
	fields := strings.Fields(cmd)
	if len(fields) == 0 {
		return "tool"
	}
	name := fields[0]
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSuffix(name, ".exe")
	if name == "" {
		return "tool"
	}
	return name
}

// SetEditor overrides $EDITOR (used by --editor).
func (m *Model) SetEditor(s string) {
	if strings.TrimSpace(s) != "" {
		m.editorOverride = s
	}
}

// editor returns the editor command to use for metadata editing.
func (m *Model) editor() string {
	if m.editorOverride != "" {
		return m.editorOverride
	}
	for _, k := range []string{"VISUAL", "EDITOR"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	for _, c := range []string{"nvim", "vim", "vi", "nano", "emacs"} {
		if _, err := lookPath(c); err == nil {
			return c
		}
	}
	return "vi"
}

// Init implements tea.Model. Metadata is extracted for every paper up front so
// the right pane is populated before the user navigates.
func (m *Model) Init() tea.Cmd { return m.extractAllCmd() }

// rebuild recomputes the visible list.
//
// The order is: the tag filter narrows the library to a set, then the fuzzy
// query ranks that set. Filtering first is what makes "!LLM && DB" mean
// "papers tagged DB but not LLM" rather than "papers whose text mentions DB".
func (m *Model) rebuild() {
	m.all = m.lib.Papers
	q := strings.TrimSpace(m.quat)

	base := m.all
	if m.filterTree != nil {
		kept := make([]*app.Paper, 0, len(base))
		for _, p := range base {
			if m.filterMatch(p) {
				kept = append(kept, p)
			}
		}
		base = kept
	}

	m.visible = m.visible[:0]
	switch {
	case q != "":
		docs, positions := m.rankingInputs(base)
		m.visible = search.OrderOf(q, docs, positions, m.metaUI)
	case len(base) == len(m.all):
		for i := range m.all {
			m.visible = append(m.visible, i)
		}
	default:
		// Filtered but not searched: keep the library order of the survivors.
		// Walking the whole library is what keeps the indices meaningful.
		for i, p := range m.all {
			if m.filterTree == nil || m.filterMatch(p) {
				m.visible = append(m.visible, i)
			}
		}
	}
	if m.cursor >= len(m.visible) {
		m.cursor = len(m.visible) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.clampOffset()
}

// filterMatch reports whether a paper satisfies the active filter.
//
// Tag names are matched case-insensitively, which is how the rest of non treats
// them: a tag typed "llm" and one typed "LLM" are the same tag.
func (m *Model) filterMatch(p *app.Paper) bool {
	if m.filterTree == nil {
		return true
	}
	return app.PaperHasTags(m.lib, p, m.filterTree)
}

// setFilterText parses a new filter expression and rebuilds. A parse error
// keeps the previous tree so the list only changes on a usable expression.
func (m *Model) setFilterText(text string) {
	m.filterText = text
	tree, err := filter.Parse(text)
	m.filterErr = err
	if err != nil {
		return
	}
	m.filterTree = tree
	m.rebuild()
}

// filterActive reports whether a filter is narrowing the list.
func (m *Model) filterActive() bool { return m.filterTree != nil }

// filterUnknown returns the tags the expression names that are not in the
// global table. They are a typo signal, not an error: the expression still
// evaluates, it simply matches nothing for those names.
func (m *Model) filterUnknown() []string {
	if m.filterTree == nil {
		return nil
	}
	known := func(name string) bool {
		_, ok := m.lib.Store.Config.Tag(name)
		return ok
	}
	return filter.UnknownTags(m.filterTree, known)
}

// clearFilter drops the filter and rebuilds.
func (m *Model) clearFilter() {
	m.filterText = ""
	m.filterTree = nil
	m.filterErr = nil
	m.fin.SetValue("")
	m.rebuild()
}

// current returns the paper under the cursor, or nil.
func (m *Model) current() *app.Paper {
	if m.cursor < 0 || m.cursor >= len(m.visible) {
		return nil
	}
	idx := m.visible[m.cursor]
	if idx < 0 || idx >= len(m.all) {
		return nil
	}
	return m.all[idx]
}

// currentName returns the file name of the paper under the cursor.
func (m *Model) currentName() string {
	if p := m.current(); p != nil {
		return p.Name
	}
	return ""
}

func (m *Model) move(delta int) tea.Cmd {
	if len(m.visible) == 0 {
		return nil
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.visible) {
		m.cursor = len(m.visible) - 1
	}
	m.details = 0
	m.clampOffset()
	return m.ensureSelectedMeta()
}

func (m *Model) clampOffset() {
	if m.listHeight() <= 0 {
		m.offset = 0
		return
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+m.listHeight() {
		m.offset = m.cursor - m.listHeight() + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// chromeHeight is the number of rows the header and footer take together.
const chromeHeight = 2

// bodyHeight is the height of the two-pane body. It mirrors what View computes
// so that scrolling in the list matches what is on screen.
func (m *Model) bodyHeight() int {
	h := m.height - chromeHeight
	if h < 3 {
		return 3
	}
	return h
}

// listHeight is the number of rows available for the file list.
func (m *Model) listHeight() int { return m.bodyHeight() }

// extractAllCmd extracts metadata for papers that have none yet. It runs in
// the background; each finished paper is reported with an extractDoneMsg.
func (m *Model) extractAllCmd() tea.Cmd {
	type job struct {
		name string
		path string
	}
	var jobs []job
	for _, p := range m.all {
		if p.Meta != nil && strings.TrimSpace(p.Meta.Title) != "" {
			continue
		}
		jobs = append(jobs, job{name: p.Name, path: m.lib.Path(p.Name)})
	}
	if len(jobs) == 0 {
		return nil
	}
	return func() tea.Msg {
		var msgs []tea.Msg
		for _, j := range jobs {
			res := extract.Extract(j.path)
			e := &meta.Entry{
				Title:    res.TitleOr(j.name),
				Author:   res.Author,
				Abstract: res.Abstract,
				Auto:     true,
			}
			msgs = append(msgs, extractDoneMsg{name: j.name, entry: e})
		}
		return extractBatchMsg{msgs: msgs}
	}
}

// extractDoneMsg carries extraction results for one paper.
type extractDoneMsg struct {
	name  string
	entry *meta.Entry
}

// extractBatchMsg groups the results of a background extraction sweep.
type extractBatchMsg struct{ msgs []tea.Msg }

// ensureSelectedMeta extracts metadata for the selected paper when it is still
// unknown, so the right pane always has something to show.
//
// The command only reads the file and returns a message; the store is updated
// from Update, which keeps all mutation on the event loop.
func (m *Model) ensureSelectedMeta() tea.Cmd {
	p := m.current()
	if p == nil || (p.Meta != nil && strings.TrimSpace(p.Meta.Title) != "") {
		return nil
	}
	name, path := p.Name, m.lib.Path(p.Name)
	return func() tea.Msg {
		return extractDoneMsg{name: name, entry: extractEntry(name, path)}
	}
}

// extractEntry builds a metadata entry from a PDF, without touching the store.
func extractEntry(name, path string) *meta.Entry {
	res := extract.Extract(path)
	return &meta.Entry{
		Title:    res.TitleOr(name),
		Author:   res.Author,
		Abstract: res.Abstract,
		Auto:     true,
	}
}

// refreshStatus recomputes the footer counter.
func (m *Model) refreshStatus() { m.rebuild() }

// statusLine is the text shown on the bottom left.
func (m *Model) statusLine() string {
	if m.status != "" {
		return m.status
	}
	n := len(m.visible)
	total := len(m.all)
	switch {
	case total == 0:
		return "no PDFs in " + m.dir
	case n == total:
		return fmt.Sprintf("%d papers", total)
	default:
		return fmt.Sprintf("%d of %d papers", n, total)
	}
}

func (m *Model) setStatus(s string) { m.status, m.statusK = s, false }
func (m *Model) setErr(s string)    { m.status, m.statusK = s, true }

// defaultTool returns the tool a plain "o" should use.
func (m *Model) defaultTool() (meta.Tool, bool) {
	return m.lib.Store.Config.EffectiveDefault()
}

// visibleNames is a helper used by tests.
func (m *Model) visibleNames() []string {
	out := make([]string, 0, len(m.visible))
	for _, i := range m.visible {
		out = append(out, m.all[i].Name)
	}
	return out
}

// titleOf is the display title shown in the details pane.
func (m *Model) titleOf(p *app.Paper) string {
	if p == nil {
		return ""
	}
	if p.Meta != nil && strings.TrimSpace(p.Meta.Title) != "" {
		return p.Meta.Title
	}
	return extract.TitleFromFilename(p.Name)
}

// AddTool installs a tool given as "name=command[,tui|gui]". It is the
// command line equivalent of the tool form in the settings modal.
func (m *Model) AddTool(spec string) error {
	tool, err := meta.ParseToolSpec(spec)
	if err != nil {
		return err
	}
	if err := m.lib.Store.Config.AddTool(tool); err != nil {
		return fmt.Errorf("%s: %w", spec, err)
	}
	return m.lib.Store.Save()
}

// confirm is a small yes/no dialog, used before an action that changes
// something the user typed (a rename that had to be normalized).
type confirm struct {
	title  string
	lines  []string
	accept string // label of the affirmative button
	onYes  func(*Model) tea.Cmd
	onNo   func(*Model) tea.Cmd
	yes    bool // the affirmative button is focused
}

// Warning is the explanatory line of the dialog, which is the last non-empty
// line before the buttons.
func (c *confirm) Warning() string {
	for _, v := range slices.Backward(c.lines) {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// rankingInputs projects the candidates into the ranking package's view and
// maps each back to its library position. Both are built in one pass: doing the
// lookup per paper would make a rebuild quadratic in the library size.
func (m *Model) rankingInputs(base []*app.Paper) ([]search.Doc, []int) {
	pos := make(map[*app.Paper]int, len(m.all))
	for i, p := range m.all {
		pos[p] = i
	}
	docs := make([]search.Doc, len(base))
	positions := make([]int, len(base))
	for i, p := range base {
		docs[i] = search.Doc{Name: p.Name, Meta: p.Meta}
		positions[i] = pos[p]
	}
	return docs, positions
}
