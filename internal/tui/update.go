package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"non/internal/app"
	"non/internal/extract"
	"non/internal/meta"
)

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.ready = true
		m.clampOffset()
		return m, nil

	case extractBatchMsg:
		for _, inner := range msg.msgs {
			if done, ok := inner.(extractDoneMsg); ok {
				m.applyExtracted(done)
			}
		}
		if err := m.lib.Store.Save(); err != nil {
			m.setErr(err.Error())
		}
		m.rebuild()
		return m, nil

	case reextractMsg:
		// Keep whatever the user wrote by hand; only the automatic fields are
		// replaced.
		e := msg.entry
		if cur, ok := m.lib.Store.Lookup(msg.name); ok && !cur.Auto {
			e.Title = cur.Title
			e.Comment = cur.Comment
			e.Tags = cur.Tags
			if cur.Author != "" {
				e.Author = cur.Author
			}
			e.Auto = false
			e.Fields = cur.Fields
		}
		if cur, ok := m.lib.Store.Lookup(msg.name); ok && cur.Auto {
			// Tags and comments survive a re-extraction even for automatic
			// entries; they cannot have been derived from the PDF.
			e.Tags = cur.Tags
			e.Comment = cur.Comment
			e.Fields = cur.Fields
		}
		if err := m.lib.SaveMeta(msg.name, e); err != nil {
			m.setErr(err.Error())
			return m, nil
		}
		m.setStatus("metadata re-extracted from " + msg.name)
		m.details = 0
		return m, nil

	case extractDoneMsg:
		m.applyExtracted(msg)
		if err := m.lib.Store.Save(); err != nil {
			m.setErr(err.Error())
		}
		m.clampOffset()
		return m, nil

	case savedMsg:
		m.setStatus(msg.text)
		m.reloadNow()
		return m, m.ensureSelectedMeta()

	case errMsg:
		m.setErr(msg.text)
		return m, nil

	case editorDoneMsg:
		return m.applyEditor(msg)

	case inspectMsg:
		m.inspect = msg.info
		m.mode = modeInspect
		m.details = 0
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	// Pass non-key messages through to whichever input is focused.
	if m.mode == modeSearch {
		var cmd tea.Cmd
		m.search, cmd = m.search.Update(msg)
		return m, cmd
	}
	if m.mode == modeRename {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	if m.mode == modeFilter {
		var cmd tea.Cmd
		m.fin, cmd = m.fin.Update(msg)
		return m, cmd
	}
	if m.mode == modeMetaForm && m.metaForm != nil {
		f := m.metaForm
		if f.focus < 0 || f.focus >= len(f.rows) {
			return m, nil
		}
		switch f.rows[f.focus].kind {
		case rowAbstract, rowComment:
			var cmd tea.Cmd
			f.rows[f.focus].area, cmd = f.rows[f.focus].area.Update(msg)
			return m, cmd
		default:
			var cmd tea.Cmd
			f.rows[f.focus].input, cmd = f.rows[f.focus].input.Update(msg)
			return m, cmd
		}
	}
	if m.mode == modeSettings && m.settings != nil {
		s := m.settings
		fields := s.fields()
		if s.focused < 0 || s.focused >= len(fields) {
			return m, nil
		}
		var cmd tea.Cmd
		fields[s.focused].input, cmd = fields[s.focused].input.Update(msg)
		return m, cmd
	}
	return m, nil
}

// applyExtracted stores a background extraction result unless the entry has
// been hand edited in the meantime.
func (m *Model) applyExtracted(msg extractDoneMsg) {
	if existing, ok := m.lib.Store.Lookup(msg.name); ok {
		if !existing.Auto && strings.TrimSpace(existing.Title) != "" {
			return // the user already filled this one in
		}
	}
	m.lib.Store.Set(msg.name, msg.entry)
	if p := m.lib.Paper(msg.name); p != nil {
		p.Meta = msg.entry
	}
}

// savedMsg reports a successful write.
type savedMsg struct{ text string }

// errMsg reports a failure.
type errMsg struct{ text string }

// reloadNow rescans the directory and rebuilds the visible list immediately.
// The listing is a single readdir, so doing it inline keeps the UI consistent
// with what the user just did (rename, save) without a round trip through a
// command.
func (m *Model) reloadNow() {
	if err := m.lib.Rescan(); err != nil {
		m.setErr(err.Error())
		return
	}
	m.rebuild()
}

type reloadMsg struct{}

// keys ---------------------------------------------------------------------

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// A message from the last action is cleared as soon as the user acts.
	if m.mode == modeBrowse {
		m.status = ""
	}

	switch m.mode {
	case modeSearch:
		return m.handleSearchKey(msg)
	case modeActions:
		return m.handleActionKey(msg)
	case modeOpener:
		return m.handleOpenerKey(msg)
	case modeRename:
		return m.handleRenameKey(msg)
	case modeInspect:
		return m.handleInspectKey(msg)
	case modeHelp:
		return m.handleHelpKey(msg)
	case modeSettings:
		return m.handleSettingsKey(msg)
	case modeMetaForm:
		return m.handleMetaFormKey(msg)
	case modeFilter:
		return m.handleFilterKey(msg)
	case modeConfirm:
		return m.handleConfirmKey(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "ctrl+d":
		m.quitting = true
		return m, tea.Quit
	case "?":
		m.mode = modeHelp
		m.status = ""
		return m, nil
	case "s":
		m.openSettings()
		return m, nil
	case "j", "down", "ctrl+n":
		return m, m.move(1)
	case "k", "up", "ctrl+p":
		return m, m.move(-1)
	case "g", "home":
		m.cursor = 0
		m.details = 0
		m.clampOffset()
		return m, m.ensureSelectedMeta()
	case "G", "end":
		m.cursor = len(m.visible) - 1
		m.details = 0
		m.clampOffset()
		return m, m.ensureSelectedMeta()
	case "ctrl+f", "pgdown":
		return m, m.move(m.listHeight())
	case "ctrl+b", "pgup":
		return m, m.move(-m.listHeight())
	case "J":
		m.details++
		return m, nil
	case "K":
		if m.details > 0 {
			m.details--
		}
		return m, nil
	case "f":
		m.mode = modeFilter
		m.fin.SetValue(m.filterText)
		m.fin.CursorEnd()
		return m, m.fin.Focus()
	case "/":
		m.mode = modeSearch
		m.search.SetValue(m.quat)
		m.search.CursorEnd()
		return m, m.search.Focus()
	case "esc", "ctrl+x":
		// Esc clears the most recent narrowing step: the search first, because
		// it is the narrower one, then the filter.
		if m.quat != "" {
			m.quat = ""
			m.search.SetValue("")
			m.rebuild()
			m.setStatus("search cleared")
			return m, m.ensureSelectedMeta()
		}
		if m.filterActive() || m.filterText != "" {
			m.clearFilter()
			m.setStatus("filter cleared")
			return m, m.ensureSelectedMeta()
		}
		return m, nil
	case "tab":
		m.metaUI = !m.metaUI
		m.rebuild()
		if m.metaUI {
			m.setStatus("search includes metadata")
		} else {
			m.setStatus("search matches file names only")
		}
		return m, m.ensureSelectedMeta()
	case "enter":
		if p := m.current(); p != nil {
			m.openActions()
		}
		return m, nil
	case "r":
		return m, m.renameCmd()
	case "m", "ctrl+e":
		return m, m.editMetaCmd()
	case "o":
		return m, m.openCmd()
	case "O":
		m.beginOpener()
		return m, nil
	case "i":
		return m, m.inspectCmd()
	case "e":
		return m, m.reextractCmd()
	case "R":
		return m, m.reloadCmd()
	}
	return m, nil
}

// search -------------------------------------------------------------------

func (m *Model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Escape leaves search mode and clears the query.
		m.mode = modeBrowse
		m.search.Blur()
		m.search.SetValue("")
		m.quat = ""
		m.rebuild()
		m.setStatus("search cleared")
		return m, m.ensureSelectedMeta()
	case "enter":
		// Enter leaves search mode and keeps the query, so the filtered list
		// stays on screen while browsing.
		m.mode = modeBrowse
		m.search.Blur()
		m.quat = m.search.Value()
		m.rebuild()
		return m, m.ensureSelectedMeta()
	case "ctrl+n", "down":
		return m, m.move(1)
	case "ctrl+p", "up":
		return m, m.move(-1)
	case "tab":
		m.metaUI = !m.metaUI
		m.quat = m.search.Value()
		m.rebuild()
		return m, m.ensureSelectedMeta()
	}
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	q := m.search.Value()
	if q != m.quat {
		m.quat = q
		m.rebuild()
		m.details = 0
		return m, tea.Batch(cmd, m.ensureSelectedMeta())
	}
	return m, cmd
}

// actions ------------------------------------------------------------------

func (m *Model) openActions() {
	p := m.current()
	if p == nil {
		return
	}
	openName := "Open with…"
	if t, ok := m.defaultTool(); ok {
		openName = "Open with " + t.Name
	}
	editName := "Edit metadata ($EDITOR)"
	if !m.lib.Store.Config.UseEditor {
		editName = "Edit metadata"
	}
	m.actions = []action{
		{name: openName, key: "o", run: func(m *Model) tea.Cmd { return m.openCmd() }},
		{name: "Open with… (choose tool)", key: "O", run: func(m *Model) tea.Cmd {
			m.beginOpener()
			return nil
		}},
		{name: "Rename", key: "r", run: func(m *Model) tea.Cmd { return m.renameCmd() }},
		{name: editName, key: "m", run: func(m *Model) tea.Cmd { return m.editMetaCmd() }},
		{name: "Inspect", key: "i", run: func(m *Model) tea.Cmd { return m.inspectCmd() }},
	}
	if p.Meta != nil && strings.TrimSpace(p.Meta.Title) != "" {
		m.actions = append(m.actions, action{name: "Re-extract metadata from PDF", key: "e",
			run: func(m *Model) tea.Cmd { return m.reextractCmd() }})
	}
	m.actions = append(m.actions, action{name: "Cancel", key: "esc", run: func(m *Model) tea.Cmd {
		m.mode = modeBrowse
		return nil
	}})
	m.actCur = 0
	m.mode = modeActions
}

func (m *Model) handleActionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.mode = modeBrowse
		return m, nil
	case "j", "down", "ctrl+n":
		m.actCur = (m.actCur + 1) % len(m.actions)
		return m, nil
	case "k", "up", "ctrl+p":
		m.actCur = (m.actCur - 1 + len(m.actions)) % len(m.actions)
		return m, nil
	case "enter":
		act := m.actions[m.actCur]
		m.mode = modeBrowse
		return m, act.run(m)
	}
	// Shortcut keys are matched case-sensitively, so "O" (choose a program)
	// stays distinct from "o" (open with the configured one).
	for _, a := range m.actions {
		if a.key != "" && msg.String() == a.key {
			m.mode = modeBrowse
			return m, a.run(m)
		}
	}
	return m, nil
}

// opener input -------------------------------------------------------------

// beginOpener picks the tool for the current paper. With a single configured
// tool it is used directly; otherwise the user chooses from the list.
func (m *Model) beginOpener() {
	tools := m.lib.Store.Config.EffectiveTools()
	if len(tools) == 0 {
		m.setErr("no tools configured — press s, then t")
		return
	}
	m.openerList = tools
	m.openerCur = 0
	if def, ok := m.lib.Store.Config.EffectiveDefault(); ok {
		for i, t := range tools {
			if strings.EqualFold(t.Name, def.Name) {
				m.openerCur = i
			}
		}
	}
	m.mode = modeOpener
}

func (m *Model) handleOpenerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.mode = modeBrowse
		return m, nil
	case "j", "down", "ctrl+n":
		if len(m.openerList) > 0 {
			m.openerCur = (m.openerCur + 1) % len(m.openerList)
		}
		return m, nil
	case "k", "up", "ctrl+p":
		if len(m.openerList) > 0 {
			m.openerCur = (m.openerCur - 1 + len(m.openerList)) % len(m.openerList)
		}
		return m, nil
	case "enter":
		if m.openerCur < 0 || m.openerCur >= len(m.openerList) {
			m.mode = modeBrowse
			return m, nil
		}
		tool := m.openerList[m.openerCur]
		m.mode = modeBrowse
		return m, m.openWith(m.currentName(), tool)
	}
	return m, nil
}

// rename input -------------------------------------------------------------

func (m *Model) renameCmd() tea.Cmd {
	p := m.current()
	if p == nil {
		return nil
	}
	m.mode = modeRename
	m.input.Prompt = "rename: "
	m.input.Placeholder = "new file name"
	m.input.SetValue(p.Name)
	m.input.CursorEnd()
	m.input.Focus()
	m.pendingEditor.name = p.Name
	return nil
}

func (m *Model) handleRenameKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = modeBrowse
		m.input.Blur()
		return m, nil
	case "enter":
		newName := strings.TrimSpace(m.input.Value())
		old := m.pendingEditor.name
		if newName == "" || newName == old {
			m.mode = modeBrowse
			m.input.Blur()
			return m, nil
		}
		// If the name has to be cleaned up, show what will happen before doing
		// it, rather than silently renaming to something the user did not type.
		change := app.CleanFilename(newName)
		if change.Changed() {
			m.mode = modeBrowse
			m.input.Blur()
			m.askRename(old, change)
			return m, nil
		}
		return m, m.doRename(old, change.Clean)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// inspect ------------------------------------------------------------------

func (m *Model) inspectCmd() tea.Cmd {
	p := m.current()
	if p == nil {
		return nil
	}
	path := m.lib.Path(p.Name)
	return func() tea.Msg {
		return inspectMsg{info: app.Inspect(path)}
	}
}

type inspectMsg struct{ info app.FileInfo }

func (m *Model) handleInspectKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "enter", "i":
		m.mode = modeBrowse
		return m, nil
	case "j", "down", "J":
		m.details++
		return m, nil
	case "k", "up", "K":
		if m.details > 0 {
			m.details--
		}
		return m, nil
	}
	return m, nil
}

// help ---------------------------------------------------------------------

func (m *Model) handleHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q", "?", "enter":
		m.mode = modeBrowse
	}
	return m, nil
}

// reload -------------------------------------------------------------------

func (m *Model) reloadCmd() tea.Cmd {
	return func() tea.Msg { return reloadMsg{} }
}

// openCmd launches the default tool for the selected paper.
func (m *Model) openCmd() tea.Cmd {
	p := m.current()
	if p == nil {
		return nil
	}
	tool, ok := m.defaultTool()
	if !ok {
		return func() tea.Msg { return errMsg{text: "no tools configured — press s, then t"} }
	}
	return m.openWith(p.Name, tool)
}

// openWith starts a tool on a paper. A gui tool is started and left alone; a
// tui tool takes over the terminal until it exits, and the TUI is repainted
// afterwards.
func (m *Model) openWith(paper string, tool meta.Tool) tea.Cmd {
	path := m.lib.Path(paper)
	argv := splitCommand(tool.Command)
	if len(argv) == 0 {
		return func() tea.Msg { return errMsg{text: "tool " + tool.Name + " has an empty command"} }
	}
	argv = append(argv, path)

	if tool.IsTUI() {
		c := execCommand(argv[0], argv[1:]...)
		return tea.ExecProcess(c, func(err error) tea.Msg {
			if err != nil {
				return errMsg{text: fmt.Sprintf("%s: %v", tool.Name, err)}
			}
			return savedMsg{text: fmt.Sprintf("closed %s", tool.Name)}
		})
	}
	return func() tea.Msg {
		if err := Launch(strings.Join(argv, " "), ""); err != nil {
			return errMsg{text: fmt.Sprintf("open: %v", err)}
		}
		return savedMsg{text: fmt.Sprintf("opened %s with %s", paper, tool.Name)}
	}
}

// reextract ----------------------------------------------------------------

// reextractCmd re-reads the PDF and replaces the automatic fields. The command
// only reads the file; the store is written from Update so that there is a
// single writer.
func (m *Model) reextractCmd() tea.Cmd {
	p := m.current()
	if p == nil {
		return nil
	}
	name, path := p.Name, m.lib.Path(p.Name)
	return func() tea.Msg {
		return reextractMsg{name: name, entry: extractEntry(name, path)}
	}
}

type reextractMsg struct {
	name  string
	entry *meta.Entry
}

// metadata editing ---------------------------------------------------------

// editMetaCmd opens the metadata file in $EDITOR. The editor is started in a
// tea.ExecProcess so the TUI is restored afterwards.
func (m *Model) editMetaCmd() tea.Cmd {
	p := m.current()
	if p == nil {
		return nil
	}
	// The built-in form is the default; $EDITOR is used when the "Open editor"
	// setting is on.
	if !m.lib.Store.Config.UseEditor {
		m.openMetaForm()
		return nil
	}
	// Build the document from what is in the store; metadata that has not been
	// extracted yet is shown with the title taken from the file name, so the
	// editor never opens with an empty (and therefore unsaveable) title.
	e := p.Meta
	if e == nil {
		e = &meta.Entry{}
	}
	var content string
	if strings.TrimSpace(e.Title) == "" {
		content = meta.TemplateMarkdown(e, extract.TitleFromFilename(p.Name))
	} else {
		content = meta.FormatMarkdown(e)
	}
	editor := m.editor()
	argv := splitCommand(editor)
	if len(argv) == 0 {
		return func() tea.Msg { return errMsg{text: "no $EDITOR configured"} }
	}
	tmp, err := writeTemp("non-*.md", content)
	if err != nil {
		return func() tea.Msg { return errMsg{text: err.Error()} }
	}
	name := p.Name
	c := execCommand(argv[0], append(argv[1:], tmp)...)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		return editorDoneMsg{name: name, path: tmp, err: err}
	})
}

type editorDoneMsg struct {
	name string
	path string
	err  error
}

// applyEditor parses the edited markdown back into the store.
func (m *Model) applyEditor(msg editorDoneMsg) (tea.Model, tea.Cmd) {
	text, err := readFile(msg.path)
	// The temp file is removed only once its content has been accepted. On a
	// rejection it is kept, and its path goes into the error, so the typing is
	// not thrown away over one bad tag.
	if msg.err != nil {
		removeFile(msg.path)
		m.setErr(fmt.Sprintf("editor: %v", msg.err))
		return m, nil
	}
	if err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	e, err := meta.ParseMarkdown(text)
	if err != nil {
		// The text does not parse: keep the file so the typing is not lost, and
		// say where it is.
		m.setErr(fmt.Sprintf("%s: %v — metadata unchanged, your text is kept at %s",
			msg.name, err, msg.path))
		return m, nil
	}
	// A tag the table does not know would put the store in the state
	// Store.Problems reports, so it is refused here too rather than written and
	// complained about afterwards.
	if bad := m.lib.Store.ValidateTags(e.Tags); len(bad) > 0 {
		m.setErr(fmt.Sprintf("%s: not in the tag table: %s — metadata unchanged, your text is kept at %s",
			msg.name, strings.Join(bad, ", "), msg.path))
		return m, nil
	}
	if err := m.lib.SaveMeta(msg.name, e); err != nil {
		m.setErr(err.Error())
		return m, nil
	}
	removeFile(msg.path)
	m.setStatus("metadata saved for " + msg.name)
	m.reloadNow()
	return m, m.ensureSelectedMeta()
}

// helpers used by the model ------------------------------------------------

// filter -------------------------------------------------------------------

// handleFilterKey edits the tag filter expression.
//
// Unlike search, the filter is live: every keystroke re-parses and re-narrows
// the list, so the effect of "LLM && " is visible while it is being typed. A
// half-typed expression leaves the last good tree in place, so the list never
// flashes empty, and the parse error is shown in the prompt line instead of
// being thrown away.
func (m *Model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		// Esc leaves the prompt and clears the filter, matching search.
		m.mode = modeBrowse
		m.fin.Blur()
		was := m.filterActive()
		m.clearFilter()
		if was {
			m.setStatus("filter cleared")
		}
		return m, m.ensureSelectedMeta()
	case "enter":
		// Enter keeps the filter, so it stays in force while browsing.
		m.mode = modeBrowse
		m.fin.Blur()
		m.filterText = m.fin.Value()
		if m.filterActive() {
			m.setStatus(m.filterSummary())
		}
		return m, m.ensureSelectedMeta()
	case "ctrl+n", "down":
		return m, m.move(1)
	case "ctrl+p", "up":
		return m, m.move(-1)
	}

	var cmd tea.Cmd
	m.fin, cmd = m.fin.Update(msg)
	if v := m.fin.Value(); v != m.filterText {
		m.setFilterText(v)
		m.details = 0
		return m, tea.Batch(cmd, m.ensureSelectedMeta())
	}
	return m, cmd
}

// filterSummary is the status message after applying a filter: how much is
// left, plus any tag the expression names that the table does not have.
func (m *Model) filterSummary() string {
	n := len(m.visible)
	s := fmt.Sprintf("filter: %d of %d", n, len(m.all))
	if unknown := m.filterUnknown(); len(unknown) > 0 {
		s += "  — no such tag: " + strings.Join(unknown, ", ")
	}
	return s
}

// rename with confirmation ------------------------------------------------

// askRename opens a yes/no dialog describing how the typed name will change.
func (m *Model) askRename(old string, change app.FilenameChange) {
	m.confirm = &confirm{
		title:  "rename",
		accept: "rename",
		yes:    true,
		lines: []string{
			"'" + change.Original + "'",
			"",
			change.Warning(),
		},
		onYes: func(m *Model) tea.Cmd { return m.doRename(old, change.Clean) },
	}
	m.mode = modeConfirm
}

// doRename performs the rename and reports the outcome. It returns only a
// command because it mutates the model in place; the caller is already holding
// this model.
func (m *Model) doRename(old, newName string) tea.Cmd {
	m.confirm = nil
	m.mode = modeBrowse
	final, err := m.lib.RenamePaper(old, newName)
	if err != nil {
		m.setErr(fmt.Sprintf("rename: %v", err))
		return nil
	}
	m.setStatus("renamed to " + final)
	m.reloadNow()
	return m.ensureSelectedMeta()
}

// handleConfirmKey drives a yes/no dialog. Enter and "y" accept, esc and "n"
// decline; left/right and tab move between the buttons, and the accepted button
// is always the safe one to press twice.
func (m *Model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	c := m.confirm
	if c == nil {
		m.mode = modeBrowse
		return m, nil
	}
	switch msg.String() {
	case "esc", "n", "N", "q":
		m.confirm = nil
		m.mode = modeBrowse
		if c.onNo != nil {
			return m, c.onNo(m)
		}
		return m, nil
	case "y", "Y":
		m.confirm = nil
		if c.onYes != nil {
			return m, c.onYes(m)
		}
		m.mode = modeBrowse
		return m, nil
	case "enter":
		m.confirm = nil
		if c.yes && c.onYes != nil {
			return m, c.onYes(m)
		}
		m.mode = modeBrowse
		if !c.yes && c.onNo != nil {
			return m, c.onNo(m)
		}
		return m, nil
	case "tab", "right", "l", "h", "left":
		c.yes = !c.yes
		return m, nil
	}
	return m, nil
}
