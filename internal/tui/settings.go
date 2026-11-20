package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"non/internal/meta"
)

// The settings modal holds the global configuration: how metadata is edited,
// whether search looks inside metadata, the list of tools used by "open with",
// and the global tag table.
//
// It is a stack of pages. Each page is either a plain form (settings, one tool,
// one tag) or a list to pick from (tools, tags). Esc always goes back one step,
// and only returns to the browser from the root page, so nothing typed is lost
// by a stray keypress.

type setPage int

const (
	pageRoot setPage = iota
	pageTools
	pageToolForm
	pageTags
	pageTagForm
)

// setField is one editable row of a settings form.
type setField struct {
	label   string
	help    string
	input   textinput.Model
	toggle  bool // rendered as on/off instead of an input
	on      bool
	options []string // when set, the value cycles through these
}

// settings is the modal state.
type settings struct {
	page    setPage
	cursor  int // selected row / list item
	off     int // scroll offset for list pages
	status  string
	statusK bool

	// root holds the settings page's own rows. It is kept separate from form,
	// because the tool and tag forms are also forms of fields: sharing one
	// slice meant leaving a form (which clears it) wiped the settings page.
	root    []setField
	form    []setField
	focused int // index of the focused input, -1 when the page has none

	// tool being edited
	toolIdx  int // -1 when adding
	toolType int
	tools    []meta.Tool

	// tag being edited
	tagIdx int
	tagOld string
	tags   []meta.Tag
	tagUse map[string]int
}

var toolTypes = []string{meta.ToolGUI, meta.ToolTUI}

// fields is the row list of the current page.
func (s *settings) fields() []setField {
	if s.page == pageRoot {
		return s.root
	}
	return s.form
}

// openSettings builds the modal from the current configuration.
func (m *Model) openSettings() {
	s := &settings{page: pageRoot, focused: -1}
	s.root = m.settingsFields()
	s.cursor = 0
	m.settings = s
	m.mode = modeSettings
}

// settingsFields builds the root form from the store config.
func (m *Model) settingsFields() []setField {
	c := m.lib.Store.Config
	editor := setField{label: "Open editor", toggle: true, on: c.UseEditor,
		help: "off: edit metadata in this app   on: use $EDITOR"}
	search := setField{label: "Search metadata", toggle: true, on: c.SearchMeta,
		help: "include author, tags and abstract in fuzzy search"}
	def := setField{label: "Default tool", options: m.toolNames(), input: textinput.New(),
		help: "the program o opens; h/l pick another"}
	def.input.SetValue(c.DefaultTool)
	editTools := setField{label: "Manage tools", help: "press enter to open the tool list"}
	editTags := setField{label: "Manage tags", help: "press enter to open the tag table"}
	return []setField{editor, search, def, editTools, editTags}
}

// toolNames lists the configured tool names, or the platform default.
func (m *Model) toolNames() []string {
	tools := m.lib.Store.Config.EffectiveTools()
	out := make([]string, 0, len(tools))
	for _, t := range tools {
		out = append(out, t.Name)
	}
	return out
}

// --- key handling ---------------------------------------------------------

func (m *Model) handleSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	s := m.settings
	if s == nil {
		m.mode = modeBrowse
		return m, nil
	}
	key := msg.String()

	if key == "ctrl+c" || key == "ctrl+d" {
		m.quitting = true
		return m, tea.Quit
	}

	// A message is feedback about the last action, not a permanent banner: any
	// keypress dismisses it and the keymap returns. Handlers that act below set
	// their own message again, so a key that does something still reports it.
	s.status = ""

	switch s.page {
	case pageRoot:
		return m.settingsRootKey(key)
	case pageTools:
		return m.settingsToolsKey(key)
	case pageToolForm:
		return m.settingsToolFormKey(msg, key)
	case pageTags:
		return m.settingsTagsKey(key)
	case pageTagForm:
		return m.settingsTagFormKey(msg, key)
	}
	return m, nil
}

func (m *Model) settingsRootKey(key string) (tea.Model, tea.Cmd) {
	s := m.settings
	switch key {
	case "esc", "q":
		m.mode = modeBrowse
		m.settings = nil
		m.rebuild()
		return m, nil
	case "j", "down", "ctrl+n":
		s.cursor = (s.cursor + 1) % maxInt(len(s.root), 1)
		return m, nil
	case "k", "up", "ctrl+p":
		s.cursor = (s.cursor - 1 + maxInt(len(s.root), 1)) % maxInt(len(s.root), 1)
		return m, nil
	case " ", "enter":
		if len(s.root) == 0 {
			return m, nil
		}
		s.cursor = clampCursor(s.cursor, len(s.root))
		f := &s.root[s.cursor]
		switch {
		case f.toggle:
			f.on = !f.on
			m.applySetting(f)
			return m, nil
		case f.options != nil:
			m.cycleOption(f, 1)
			return m, nil
		case f.label == "Manage tools":
			s.page = pageTools
			s.cursor = 0
			s.off = 0
			s.focused = -1
			m.refreshTools()
			return m, nil
		case f.label == "Manage tags":
			s.page = pageTags
			s.cursor = 0
			s.off = 0
			s.focused = -1
			m.refreshTags()
			return m, nil
		}
	case "l", "right":
		if len(s.root) > 0 {
			if f := &s.root[clampCursor(s.cursor, len(s.root))]; f.options != nil {
				m.cycleOption(f, 1)
				return m, nil
			}
		}
	case "h", "left":
		if len(s.root) > 0 {
			if f := &s.root[clampCursor(s.cursor, len(s.root))]; f.options != nil {
				m.cycleOption(f, -1)
				return m, nil
			}
		}
	}
	return m, nil
}

// rootRow finds the index of a root settings row by label, so pages return to
// the row that opened them instead of a hard-coded position.
func rootRow(fields []setField, label string) int {
	for i, f := range fields {
		if f.label == label {
			return i
		}
	}
	return 0
}

// cycleOption moves an options field through its values.
func (m *Model) cycleOption(f *setField, dir int) {
	if len(f.options) == 0 {
		return
	}
	cur := -1
	for i, o := range f.options {
		if strings.EqualFold(o, f.input.Value()) {
			cur = i
			break
		}
	}
	next := (cur + dir + len(f.options)) % len(f.options)
	if cur < 0 {
		next = 0
	}
	f.input.SetValue(f.options[next])
	m.applySetting(f)
}

// applySetting writes a settings field through to the store and saves.
func (m *Model) applySetting(f *setField) {
	c := &m.lib.Store.Config
	switch f.label {
	case "Open editor":
		c.UseEditor = f.on
	case "Search metadata":
		c.SearchMeta = f.on
		m.metaUI = f.on
		m.rebuild()
	case "Default tool":
		c.DefaultTool = f.input.Value()
	}
	if err := m.lib.Store.Save(); err != nil {
		m.settings.setErr(err.Error())
		return
	}
	m.settings.setStatus("saved")
}

// --- tools ----------------------------------------------------------------

func (m *Model) settingsToolsKey(key string) (tea.Model, tea.Cmd) {
	s := m.settings
	n := len(s.tools)
	switch key {
	case "esc", "q":
		s.page = pageRoot
		s.cursor = rootRow(m.settings.root, "Manage tools")
		return m, nil
	case "j", "down", "ctrl+n":
		if n > 0 {
			s.cursor = (s.cursor + 1) % n
			m.settingsClamp()
		}
		return m, nil
	case "k", "up", "ctrl+p":
		if n > 0 {
			s.cursor = (s.cursor - 1 + n) % n
			m.settingsClamp()
		}
		return m, nil
	case "a", "n":
		return m, m.beginToolForm(-1)
	case "e", "enter":
		if s.cursor < n {
			return m, m.beginToolForm(s.cursor)
		}
		return m, nil
	case "d", "delete":
		if s.cursor < n {
			name := s.tools[s.cursor].Name
			m.lib.Store.Config.RemoveTool(name)
			if err := m.lib.Store.Save(); err != nil {
				s.setErr(err.Error())
				return m, nil
			}
			m.refreshTools()
			s.setStatus("removed tool " + name)
			if s.cursor >= len(s.tools) {
				s.cursor = maxInt(len(s.tools)-1, 0)
			}
			m.refreshDefaultField()
		}
		return m, nil
	case "s":
		// Star the selected tool as the default.
		if s.cursor < n {
			name := s.tools[s.cursor].Name
			m.lib.Store.Config.DefaultTool = name
			if err := m.lib.Store.Save(); err != nil {
				s.setErr(err.Error())
				return m, nil
			}
			s.setStatus("default tool is now " + name)
			m.refreshDefaultField()
		}
		return m, nil
	}
	return m, nil
}

// beginToolForm opens the tool form for an existing tool (idx >= 0) or a new
// one (idx < 0).
func (m *Model) beginToolForm(idx int) tea.Cmd {
	s := m.settings
	s.page = pageToolForm
	s.toolIdx = idx
	s.cursor = 0
	s.focused = -1
	s.status = ""
	if idx >= 0 && idx < len(s.tools) {
		t := s.tools[idx]
		s.toolType = indexOfType(t.Type)
		s.form = []setField{
			{label: "Name", input: newInput(t.Name, "e.g. zathura"), help: "shown in the tool picker"},
			{label: "Command", input: newInput(t.Command, "e.g. zathura --fork"), help: "run as COMMAND <path>"},
			{label: "Type", options: toolTypes, input: newInput(meta.NormalizeToolType(t.Type), ""), help: "gui: detach   tui: take over the terminal"},
		}
		return s.focusFormField()
	}
	s.toolType = 0
	s.form = []setField{
		{label: "Name", input: newInput("", "e.g. zathura"), help: "shown in the tool picker"},
		{label: "Command", input: newInput("", "e.g. zathura --fork"), help: "run as COMMAND <path>"},
		{label: "Type", options: toolTypes, input: newInput(meta.ToolGUI, ""), help: "gui: detach   tui: take over the terminal"},
	}
	return s.focusFormField()
}

func newInput(value, placeholder string) textinput.Model {
	ti := textinput.New()
	ti.CharLimit = 512
	ti.Placeholder = placeholder
	ti.SetValue(value)
	return ti
}

func indexOfType(t string) int {
	if meta.NormalizeToolType(t) == meta.ToolTUI {
		return 1
	}
	return 0
}

func (m *Model) settingsToolFormKey(msg tea.KeyMsg, key string) (tea.Model, tea.Cmd) {
	s := m.settings
	switch key {
	case "esc":
		s.page = pageTools
		s.cursor = 0
		s.form = nil
		s.focused = -1
		return m, nil
	case "tab", "shift+tab", "down":
		s.cursor = (s.cursor + 1) % len(s.fields())
		return m, s.focusFormField()
	case "up":
		s.cursor = (s.cursor - 1 + len(s.fields())) % len(s.fields())
		return m, s.focusFormField()
	case "enter":
		if s.cursor < len(s.fields())-1 {
			s.cursor++
			return m, s.focusFormField()
		}
		return m.saveToolForm()
	case "ctrl+s":
		return m.saveToolForm()
	case " ":
		if f := &s.fields()[s.cursor]; f.options != nil {
			m.settingsCycleField(f)
			return m, nil
		}
	}
	if f := &s.fields()[s.cursor]; f.options != nil && (key == "l" || key == "h" || key == "right" || key == "left") {
		dir := 1
		if key == "h" || key == "left" {
			dir = -1
		}
		m.settingsCycleFieldDir(f, dir)
		return m, nil
	}
	var cmd tea.Cmd
	s.fields()[s.cursor].input, cmd = s.fields()[s.cursor].input.Update(msg)
	return m, cmd
}

// focusFormField focuses the input of the current form row.
func (s *settings) focusFormField() tea.Cmd {
	s.focused = -1
	var cmd tea.Cmd
	for i := range s.fields() {
		if i == s.cursor {
			s.focused = i
			cmd = s.fields()[i].input.Focus()
			continue
		}
		// Never return early: every other field must be blurred, or it keeps
		// drawing its cursor while the new one is focused.
		s.fields()[i].input.Blur()
	}
	return cmd
}

// settingsCycleField moves an options field to its next value.
func (m *Model) settingsCycleField(f *setField) { m.settingsCycleFieldDir(f, 1) }

func (m *Model) settingsCycleFieldDir(f *setField, dir int) {
	if len(f.options) == 0 {
		return
	}
	cur := 0
	for i, o := range f.options {
		if strings.EqualFold(o, f.input.Value()) {
			cur = i
			break
		}
	}
	f.input.SetValue(f.options[(cur+dir+len(f.options))%len(f.options)])
}

// saveToolForm validates and stores the tool being edited.
func (m *Model) saveToolForm() (tea.Model, tea.Cmd) {
	s := m.settings
	fields := s.form
	if len(fields) < 3 {
		s.setErr("the tool form is not open")
		return m, nil
	}
	name := strings.TrimSpace(fields[0].input.Value())
	cmd := strings.TrimSpace(fields[1].input.Value())
	typ := meta.NormalizeToolType(fields[2].input.Value())
	if name == "" {
		s.setErr("a tool needs a name")
		return m, nil
	}
	if cmd == "" {
		s.setErr("a tool needs a command")
		return m, nil
	}
	c := &m.lib.Store.Config
	if s.toolIdx >= 0 && s.toolIdx < len(s.tools) {
		old := s.tools[s.toolIdx].Name
		// Renaming must not create a duplicate.
		if !strings.EqualFold(name, old) {
			if _, exists := c.Tool(name); exists {
				s.setErr("a tool named " + name + " already exists")
				return m, nil
			}
		}
		wasDefault := strings.EqualFold(c.DefaultTool, old)
		c.Tools[s.toolIdx] = meta.Tool{Name: name, Command: cmd, Type: typ}
		if wasDefault {
			c.DefaultTool = name
		}
	} else {
		if _, exists := c.Tool(name); exists {
			s.setErr("a tool named " + name + " already exists")
			return m, nil
		}
		if err := c.AddTool(meta.Tool{Name: name, Command: cmd, Type: typ}); err != nil {
			s.setErr(err.Error())
			return m, nil
		}
	}
	if err := m.lib.Store.Save(); err != nil {
		s.setErr(err.Error())
		return m, nil
	}
	m.refreshTools()
	s.page = pageTools
	s.cursor = 0
	s.form = nil
	s.focused = -1
	s.setStatus("saved tool " + name)
	m.refreshDefaultField()
	return m, nil
}

// refreshTools reloads the tool list from the store.
func (m *Model) refreshTools() {
	m.settings.tools = append([]meta.Tool(nil), m.lib.Store.Config.EffectiveTools()...)
}

// refreshDefaultField keeps the root form in sync after tool changes.
func (m *Model) refreshDefaultField() {
	if m.settings == nil {
		return
	}
	for i := range m.settings.root {
		if m.settings.root[i].label == "Default tool" {
			m.settings.root[i].options = m.toolNames()
			m.settings.root[i].input.SetValue(m.lib.Store.Config.DefaultTool)
		}
	}
}

func (s *settings) setStatus(msg string) { s.status, s.statusK = msg, false }
func (s *settings) setErr(msg string)    { s.status, s.statusK = msg, true }

func (m *Model) settingsClamp() {
	s := m.settings
	h := m.settingsListHeight()
	if s.cursor < s.off {
		s.off = s.cursor
	}
	if s.cursor >= s.off+h {
		s.off = s.cursor - h + 1
	}
	if s.off < 0 {
		s.off = 0
	}
}

// settingsListHeight is the number of list rows the modal can show. The column
// head costs one row inside the body; the border, separator and hint are
// already deducted by modalBodyHeight.
func (m *Model) settingsListHeight() int {
	h := m.modalBodyHeight() - 1
	if h < 1 {
		return 1
	}
	return h
}

// --- tags -----------------------------------------------------------------

// refreshTags reloads the tag table and its usage counts.
func (m *Model) refreshTags() {
	s := m.settings
	s.tags = m.lib.Store.Config.Tags()
	s.tagUse = m.lib.Store.TagsInUse()
}

func (m *Model) settingsTagsKey(key string) (tea.Model, tea.Cmd) {
	s := m.settings
	n := len(s.tags)
	switch key {
	case "esc", "q":
		s.page = pageRoot
		s.cursor = rootRow(m.settings.root, "Manage tags")
		return m, nil
	case "j", "down", "ctrl+n":
		if n > 0 {
			s.cursor = (s.cursor + 1) % n
			m.settingsClamp()
		}
		return m, nil
	case "k", "up", "ctrl+p":
		if n > 0 {
			s.cursor = (s.cursor - 1 + n) % n
			m.settingsClamp()
		}
		return m, nil
	case "a", "n":
		return m, m.beginTagForm(-1)
	case "e", "enter":
		if s.cursor < n {
			return m, m.beginTagForm(s.cursor)
		}
		return m, nil
	case "d", "delete":
		if s.cursor < n {
			name := s.tags[s.cursor].Name
			used := s.tagUse[name]
			m.lib.Store.DeleteTag(name)
			if err := m.lib.Store.Save(); err != nil {
				s.setErr(err.Error())
				return m, nil
			}
			m.refreshTags()
			m.pendingEditor.name = "" // force a refresh of the cached papers
			if used > 0 {
				s.setStatus(fmt.Sprintf("removed %s from %d paper(s)", name, used))
			} else {
				s.setStatus("removed tag " + name)
			}
			if s.cursor >= len(s.tags) {
				s.cursor = maxInt(len(s.tags)-1, 0)
			}
		}
		return m, nil
	}
	return m, nil
}

func (m *Model) beginTagForm(idx int) tea.Cmd {
	s := m.settings
	s.page = pageTagForm
	s.tagIdx = idx
	s.cursor = 0
	s.focused = -1
	s.status = ""
	if idx >= 0 && idx < len(s.tags) {
		t := s.tags[idx]
		s.tagOld = t.Name
		s.form = []setField{
			{label: "Name", input: newInput(t.Name, "e.g. kvcache"), help: "letters, digits and underscore only"},
			{label: "Description", input: newInput(t.Description, "when to use this tag"), help: "when to use this tag"},
		}
		return s.focusFormField()
	} else {
		s.tagOld = ""
		s.form = []setField{
			{label: "Name", input: newInput("", "e.g. kvcache"), help: "letters, digits and underscore only"},
			{label: "Description", input: newInput("", "when to use this tag"), help: "when to use this tag"},
		}
	}
	return s.focusFormField()
}

func (m *Model) settingsTagFormKey(msg tea.KeyMsg, key string) (tea.Model, tea.Cmd) {
	s := m.settings
	switch key {
	case "esc":
		s.page = pageTags
		s.form = nil
		s.focused = -1
		return m, nil
	case "tab", "down", "enter":
		if s.cursor < len(s.fields())-1 {
			s.cursor++
			return m, s.focusFormField()
		}
		return m.saveTagForm()
	case "shift+tab", "up":
		s.cursor = (s.cursor - 1 + len(s.fields())) % len(s.fields())
		return m, s.focusFormField()
	case "ctrl+s":
		return m.saveTagForm()
	}
	var cmd tea.Cmd
	s.fields()[s.cursor].input, cmd = s.fields()[s.cursor].input.Update(msg)
	return m, cmd
}

func (m *Model) saveTagForm() (tea.Model, tea.Cmd) {
	s := m.settings
	fields := s.form
	if len(fields) < 2 {
		s.setErr("the tag form is not open")
		return m, nil
	}
	name := strings.TrimSpace(fields[0].input.Value())
	desc := strings.TrimSpace(fields[1].input.Value())
	if err := meta.CheckTagName(name); err != nil {
		s.setErr(err.Error())
		return m, nil
	}
	c := &m.lib.Store.Config
	if !strings.EqualFold(name, s.tagOld) {
		if _, exists := c.Tag(name); exists {
			s.setErr("a tag named " + name + " already exists")
			return m, nil
		}
	}
	if s.tagIdx >= 0 && s.tagIdx < len(s.tags) && s.tagOld != "" {
		// Rename follows through to every paper that uses the tag.
		if !strings.EqualFold(s.tagOld, name) {
			m.lib.Store.RenameTag(s.tagOld, name)
		}
		if idx := m.tagIndex(name); idx >= 0 {
			m.lib.Store.Config.SetTag(idx, meta.Tag{Name: name, Description: desc})
		}
	} else {
		m.lib.Store.Config.SetTag(-1, meta.Tag{Name: name, Description: desc})
	}
	if err := m.lib.Store.Save(); err != nil {
		s.setErr(err.Error())
		return m, nil
	}
	if err := m.lib.Rescan(); err != nil {
		s.setErr(err.Error())
	}
	m.refreshTags()
	s.page = pageTags
	s.form = nil
	s.focused = -1
	s.setStatus("saved tag " + name)
	return m, nil
}

func (m *Model) tagIndex(name string) int {
	for i, t := range m.lib.Store.Config.TagTable {
		if strings.EqualFold(t.Name, name) {
			return i
		}
	}
	return -1
}

// clampCursor keeps a cursor inside a list, so a stale index left by a page
// transition cannot index out of range.
func clampCursor(cursor, n int) int {
	if n <= 0 {
		return 0
	}
	if cursor < 0 {
		return 0
	}
	if cursor >= n {
		return n - 1
	}
	return cursor
}
