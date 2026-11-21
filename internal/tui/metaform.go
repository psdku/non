package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"non/internal/app"
	"non/internal/extract"
	"non/internal/meta"
)

// The built-in metadata form is the alternative to $EDITOR. It exists so the
// tags a paper carries can be constrained: the tag row is a checklist built
// from the global tag table, not a free text field, so a typo cannot invent a
// tag that the table does not know about.

// metaRow is one editable row of the form.
type metaRow struct {
	label  string
	kind   metaRowKind
	input  textinput.Model
	area   textarea.Model
	tagPos int // cursor inside the checklist row
}

type metaRowKind int

const (
	rowTitle metaRowKind = iota
	rowAuthor
	rowAbstract
	rowComment
	rowExtra    // a user supplied field, kept under its own key
	rowTags     // checklist against the tag table
	rowNewTag   // free text row that adds a tag to the table
	rowNewField // free text row that adds an extra field
	rowAction   // "save" row
	rowSection  // a non-editable divider
)

// metaForm is the state of the inline editor.
type metaForm struct {
	name  string
	rows  []metaRow
	cur   int
	focus int // index of the focused input/textarea, -1 for none

	tags  []meta.Tag // global table, sorted
	tagOn []bool     // selection matching tags
	// tagBad marks checklist entries that are not in the tag table. They are
	// kept in the file (deleting a typo would destroy intent) but must be
	// resolved before saving: either add them in the settings tag table, or
	// untick them here.
	tagBad   []bool
	tagCount map[string]int
	// tagQuery filters the checklist. With a large table the picker is a list to
	// search and scroll, not a wall of chips: this keeps it navigable when there
	// are hundreds of tags.
	tagQuery string
	// tagView is the indices of f.tags currently visible (all of them when the
	// query is empty). The row cursor indexes into this, so a filter never
	// moves the underlying selection.
	tagView []int
	tagOff  int // first visible line of the checklist
	// tagTyping is true while the checklist is in text mode, so a space types a
	// space instead of toggling a tag.
	tagTyping bool
	tagInput  textinput.Model

	status  string
	statusK bool
	off     int
}

// openMetaForm builds the form for the selected paper.
func (m *Model) openMetaForm() {
	p := m.current()
	if p == nil {
		return
	}
	e := p.Meta
	if e == nil {
		e = &meta.Entry{}
	}
	title := e.Title
	if strings.TrimSpace(title) == "" {
		title = extract.TitleFromFilename(p.Name)
	}
	f := &metaForm{name: p.Name, focus: -1}
	f.tags = m.lib.Store.Config.Tags()
	f.tagCount = m.lib.Store.TagsInUse()
	f.tagOn = make([]bool, len(f.tags))
	for i, t := range f.tags {
		for _, used := range e.Tags {
			if strings.EqualFold(used, t.Name) {
				f.tagOn[i] = true
			}
		}
	}
	// Tags the table does not know are appended to the checklist so they are
	// visible and removable, rather than silently missing from the form.
	f.tagInput = textinput.New()
	f.tagInput.Prompt = ""
	f.tagInput.Placeholder = "type to filter, enter to keep"
	f.tagInput.CharLimit = 128
	f.tagInput.Blur()
	f.tagBad = make([]bool, len(f.tags))
	for _, used := range meta.NormalizeTags(e.Tags) {
		if _, ok := m.lib.Store.Config.Tag(used); ok && !meta.CriticalTagName(used) {
			// A known tag with an untidy name is still a tag: it is shown, not
			// marked, because nothing about it is an error. --check reports it.
			continue
		}
		f.tags = append(f.tags, meta.Tag{Name: used})
		f.tagOn = append(f.tagOn, true)
		f.tagBad = append(f.tagBad, true)
	}

	f.rows = []metaRow{
		{label: "Title", kind: rowTitle, input: newInput(title, "required")},
		{label: "Author", kind: rowAuthor, input: newInput(e.Author, "comma separated")},
		{label: "Abstract", kind: rowAbstract, area: newArea(e.Abstract)},
		{label: "Comment", kind: rowComment, area: newArea(e.Comment)},
	}
	for _, extra := range extraFieldNames(e) {
		f.rows = append(f.rows, metaRow{
			label: extra, kind: rowExtra, area: newArea(e.FieldString(extra)),
		})
	}
	f.rebuildTagView()
	f.rows = append(f.rows, metaRow{label: "Tags", kind: rowTags})
	f.rows = append(f.rows, metaRow{label: "Add tag", kind: rowNewTag, input: newInput("", "type a name and press enter")})
	f.rows = append(f.rows, metaRow{
		label: "New field", kind: rowNewField,
		input: newInput("", "name=value"),
	})
	f.rows = append(f.rows, metaRow{label: "", kind: rowAction})

	m.metaForm = f
	m.mode = modeMetaForm
	m.moveMetaRow(0)
}

func newArea(value string) textarea.Model {
	ta := textarea.New()
	ta.Placeholder = ""
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.SetValue(value)
	ta.SetHeight(areaHeight(value))
	return ta
}

// areaHeight sizes a text box to its content, within limits, so a long abstract
// does not push every other row off screen.
func areaHeight(value string) int {
	n := strings.Count(value, "\n") + 1
	if n < 3 {
		return 3
	}
	if n > maxAreaHeight {
		return maxAreaHeight
	}
	return n
}

// maxAreaHeight caps a multi-line row so the form still fits on a screen.
const maxAreaHeight = 10

// areaBudget is how tall a multi-line row may get on a terminal h rows tall:
// a third of the screen, so the surrounding rows stay reachable. The text box
// scrolls internally past that.
func areaBudget(h int) int {
	c := min(max(h/4, 3), maxAreaHeight)
	return c
}

// fitAreaHeight grows a text box to fit the value wrapped at the given width,
// up to maxAreaHeight lines, so long paragraphs are readable in place.
func fitAreaHeight(ta *textarea.Model, value string, width, cap int) {
	if width < 10 {
		width = 10
	}
	n := 0
	for para := range strings.SplitSeq(value, "\n") {
		w := lipgloss.Width(para)
		lines := max((w+width-1)/width, 1)
		n += lines
	}
	if n < 3 {
		n = 3
	}
	if n > cap {
		n = cap
	}
	if ta.Height() != n {
		ta.SetHeight(n)
	}
}

// extraFieldNames lists the extra fields of an entry, sorted. Every key in
// Fields is extra by definition; the standard fields are struct members.
func extraFieldNames(e *meta.Entry) []string {
	var out []string
	for k := range e.Fields {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

// --- navigation -----------------------------------------------------------

// selectable reports whether a row can hold the cursor.
func (f *metaForm) selectable(i int) bool {
	return i >= 0 && i < len(f.rows) && f.rows[i].kind != rowSection
}

// moveMetaRow moves the cursor by delta, skipping dividers.
func (m *Model) moveMetaRow(delta int) {
	f := m.metaForm
	if f == nil || len(f.rows) == 0 {
		return
	}
	if delta == 0 {
		if !f.selectable(f.cur) {
			f.cur = 0
		}
	} else {
		// Wrap around, so a short form can be driven with one key.
		n := len(f.rows)
		for range n {
			f.cur = ((f.cur+delta)%n + n) % n
			if f.selectable(f.cur) {
				break
			}
		}
	}
	m.focusMetaRow()
}

// focusMetaRow points the focused widget at the current row.
func (m *Model) focusMetaRow() {
	f := m.metaForm
	f.focus = -1
	for i := range f.rows {
		if i != f.cur {
			continue
		}
		switch f.rows[i].kind {
		case rowTitle, rowAuthor, rowNewTag, rowNewField:
			f.focus = i
			f.rows[i].input.Focus()
		case rowAbstract, rowComment, rowExtra:
			f.focus = i
			f.rows[i].area.Focus()
		}
	}
	// Every widget that is not the focused one must be blurred. A text input
	// keeps drawing its cursor until it is blurred, which left a phantom cursor
	// in the field the user had just left.
	for i := range f.rows {
		if i == f.focus {
			continue
		}
		f.rows[i].input.Blur()
		f.rows[i].area.Blur()
	}
	// The picker's filter line is focused only while it is being typed into.
	if f.tagTyping && f.cur >= 0 && f.cur < len(f.rows) && f.rows[f.cur].kind == rowTags {
		f.tagInput.Focus()
	} else {
		f.tagInput.Blur()
	}
	// Leaving the picker ends its text mode, so a later j/k is a move again.
	if f.cur < 0 || f.cur >= len(f.rows) || f.rows[f.cur].kind != rowTags {
		f.tagTyping = false
	}
}

// --- keys -----------------------------------------------------------------

func (m *Model) handleMetaFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.metaForm
	if f == nil {
		m.mode = modeBrowse
		return m, nil
	}
	key := msg.String()
	// A filter being typed owns every key, including esc, tab and the arrows.
	// Handling it here rather than further down is what lets a tag name with a
	// space be searched for: " " must reach the query, not toggle a tag, and esc
	// must leave the filter rather than close the whole form.
	if f.tagTyping {
		return m.metaTagsKey(msg, key)
	}
	// Feedback is about the last action, not a permanent banner: any keypress
	// clears it and the hint line returns. A handler that acts below sets its
	// own message again, so an effective key still reports what it did.
	f.status = ""
	// A multi-line row keeps the arrow keys for its own cursor; rows are
	// changed with tab (and ctrl+n/p) from there.
	if row := f.rows[f.cur]; row.kind == rowAbstract || row.kind == rowComment || row.kind == rowExtra {
		if key == "up" || key == "down" || key == "left" || key == "right" {
			var cmd tea.Cmd
			f.rows[f.cur].area, cmd = f.rows[f.cur].area.Update(msg)
			return m, cmd
		}
	}
	switch key {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "esc":
		m.mode = modeBrowse
		m.metaForm = nil
		m.setStatus("metadata unchanged")
		return m, nil
	case "ctrl+s":
		return m.saveMetaForm()
	case "tab", "down", "ctrl+n":
		m.moveMetaRow(1)
		return m, nil
	case "shift+tab", "up", "ctrl+p":
		m.moveMetaRow(-1)
		return m, nil
	case "enter":
		row := f.rows[f.cur]
		switch row.kind {
		case rowAction:
			return m.saveMetaForm()
		case rowNewTag:
			name := strings.TrimSpace(row.input.Value())
			if name == "" {
				return m, nil
			}
			if m.metaFormAddTag(name) {
				f.rows[f.cur].input.SetValue("")
			}
			return m, nil
		case rowNewField:
			key, value, ok := strings.Cut(row.input.Value(), "=")
			key = strings.TrimSpace(key)
			if !ok || key == "" {
				f.setErr("write name=value to add a field")
				return m, nil
			}
			m.metaFormAddField(key, strings.TrimSpace(value))
			return m, nil
		case rowAbstract, rowComment, rowExtra:
			// Enter inserts a newline in the multi-line fields.
			var cmd tea.Cmd
			f.rows[f.cur].area, cmd = f.rows[f.cur].area.Update(msg)
			return m, cmd
		}
		// A single-line field: enter moves on rather than saving, so a stray
		// keypress does not close the form.
		m.moveMetaRow(1)
		return m, nil
	}

	switch f.rows[f.cur].kind {
	case rowTags:
		return m.metaTagsKey(msg, key)
	case rowTitle, rowAuthor, rowNewTag, rowNewField:
		var cmd tea.Cmd
		f.rows[f.cur].input, cmd = f.rows[f.cur].input.Update(msg)
		return m, cmd
	case rowAbstract, rowComment, rowExtra:
		var cmd tea.Cmd
		f.rows[f.cur].area, cmd = f.rows[f.cur].area.Update(msg)
		return m, cmd
	}
	return m, nil
}

// metaTagsKey handles the picker row.
//
// There are two states. Normally the keys are commands: space toggles, j/k and
// the arrows move, "/" starts a filter. While typing a filter every printable
// key belongs to the query, so a tag name with a space can be searched for; the
// filter stays applied after enter, and esc clears it.
func (m *Model) metaTagsKey(msg tea.KeyMsg, key string) (tea.Model, tea.Cmd) {
	f := m.metaForm
	if f.tagTyping {
		switch key {
		case "enter":
			// Enter keeps the query and esc clears it, the same as the search
			// and filter boxes on the main display. Two prompts in one program
			// behaving in opposite ways is worse than either choice.
			f.tagTyping = false
			f.tagInput.Blur()
			return m, nil
		case "esc":
			f.tagQuery = ""
			f.tagInput.SetValue("")
			f.tagTyping = false
			f.tagInput.Blur()
			f.rebuildTagView()
			return m, nil
		case "down", "ctrl+n":
			f.tagTyping = false
			f.tagInput.Blur()
			m.metaFormMoveTag(1)
			return m, nil
		case "up", "ctrl+p":
			f.tagTyping = false
			f.tagInput.Blur()
			m.metaFormMoveTag(-1)
			return m, nil
		}
		var cmd tea.Cmd
		f.tagInput, cmd = f.tagInput.Update(msg)
		f.tagQuery = f.tagInput.Value()
		f.rebuildTagView()
		return m, cmd
	}

	switch key {
	case "/":
		f.tagTyping = true
		f.tagInput.Focus()
		f.tagInput.SetValue(f.tagQuery)
		f.tagInput.CursorEnd()
		return m, nil
	case " ", "x":
		m.metaFormToggleTag()
		return m, nil
	case "j", "right", "l", "tab":
		m.metaFormMoveTag(1)
		return m, nil
	case "k", "left", "h", "shift+tab":
		m.metaFormMoveTag(-1)
		return m, nil
	}
	return m, nil
}

// --- saving ---------------------------------------------------------------

func (m *Model) saveMetaForm() (tea.Model, tea.Cmd) {
	f := m.metaForm
	e := &meta.Entry{}
	type extra struct{ key, value string }
	var extras []extra
	var tags []string
	for _, r := range f.rows {
		switch r.kind {
		case rowTitle:
			e.Title = strings.TrimSpace(r.input.Value())
		case rowAuthor:
			e.Author = strings.TrimSpace(r.input.Value())
		case rowAbstract:
			e.Abstract = strings.TrimSpace(r.area.Value())
		case rowComment:
			e.Comment = strings.TrimSpace(r.area.Value())
		case rowExtra:
			extras = append(extras, extra{r.label, strings.TrimSpace(r.area.Value())})
		}
	}
	if e.Title == "" {
		f.setErr("a title is required")
		return m, nil
	}
	// An unknown tag cannot be saved back: either add it to the tag table
	// (settings) or untick it here. Refusing is the only option that neither
	// silently deletes the tag nor writes an entry the table does not cover.
	// Only a tag that cannot be used at all blocks saving: either the table does
	// not know it, or its name would break a query. An untidy-but-known name is
	// written as it is.
	var bad []string
	for i, on := range f.tagOn {
		if i < len(f.tagBad) && f.tagBad[i] && on {
			bad = append(bad, f.tags[i].Name)
		}
	}
	if len(bad) > 0 {
		f.setErr("unusable tag(s): " + strings.Join(bad, ", ") +
			" — untick with space, or add them in settings")
		return m, nil
	}
	for i, on := range f.tagOn {
		if on && (i >= len(f.tagBad) || !f.tagBad[i]) {
			tags = append(tags, f.tags[i].Name)
		}
	}
	e.Tags = tags

	// Fields the form does not show are carried over untouched; the ones it
	// does show are written from the boxes (an emptied box removes the field).
	if cur, ok := m.lib.Store.Lookup(f.name); ok {
		e.Auto = cur.Auto && e.Title == cur.Title
		for k, v := range cur.Fields {
			e.SetRawField(k, v)
		}
	}
	for _, x := range extras {
		if x.value == "" {
			e.DeleteField(x.key)
			continue
		}
		e.SetField(x.key, x.value)
	}

	if err := m.lib.SaveMeta(f.name, e); err != nil {
		f.setErr(err.Error())
		return m, nil
	}
	m.mode = modeBrowse
	m.metaForm = nil
	m.setStatus("metadata saved for " + f.name)
	m.reloadNow()
	return m, m.ensureSelectedMeta()
}

func (f *metaForm) setErr(msg string)    { f.status, f.statusK = msg, true }
func (f *metaForm) setStatus(msg string) { f.status, f.statusK = msg, false }

// metaFormAddTag adds a tag to the table (and selects it) from the free text
// row, so a new tag never appears on a paper without being in the table.
func (m *Model) metaFormAddTag(name string) bool {
	f := m.metaForm
	name = strings.TrimSpace(name)
	if err := meta.CheckTagName(name); err != nil {
		f.setErr(err.Error())
		return false
	}
	// A name that is already in the table is not an error: the intent -- "this
	// paper should carry this tag" -- is fulfilled either way, so the tag is
	// selected and a warning explains that it already existed. Refusing outright
	// would be pedantic about something the user cannot see the harm in.
	existing, duplicate := m.lib.Store.Config.Tag(name)
	if duplicate {
		name = existing.Name
	}
	canonical, added := m.lib.Store.Config.EnsureTag(name)
	if added {
		if err := m.lib.Store.Save(); err != nil {
			f.setErr(err.Error())
			return false
		}
	}
	// Reload the checklist with the new tag selected.
	sel := map[string]bool{}
	for i, on := range f.tagOn {
		if on {
			sel[strings.ToLower(f.tags[i].Name)] = true
		}
	}
	sel[strings.ToLower(canonical)] = true
	f.tags = m.lib.Store.Config.Tags()
	f.tagOn = make([]bool, len(f.tags))
	for i, t := range f.tags {
		f.tagOn[i] = sel[strings.ToLower(t.Name)]
	}
	switch {
	case added:
		f.setStatus("added tag " + canonical)
	case duplicate:
		f.setErr("tag " + canonical + " already exists — selected it instead")
	default:
		f.setStatus("selected tag " + canonical)
	}
	return true
}

// metaFormToggleTag flips the highlighted tag.
func (m *Model) metaFormToggleTag() {
	f := m.metaForm
	i := f.tagCursor()
	if i < 0 || i >= len(f.tagOn) {
		return
	}
	f.tagOn[i] = !f.tagOn[i]
}

// metaFormMoveTag moves the checklist cursor.
func (m *Model) metaFormMoveTag(delta int) {
	f := m.metaForm
	r := &f.rows[f.cur]
	n := len(f.tagView)
	if n == 0 {
		return
	}
	c := r.tagPos + delta
	if c < 0 {
		c = n - 1
	}
	if c >= n {
		c = 0
	}
	r.tagPos = c
	f.tagOff = clampTagOff(f.tagOff, c, n, m.tagPickerRows())
}

// rebuildTagView recomputes the visible indices from the query, keeping the
// cursor inside the new view and as close as possible to where it was.
func (f *metaForm) rebuildTagView() {
	prev := f.tagCursor()
	var sel string
	if prev >= 0 && prev < len(f.tagView) && f.tagView[prev] < len(f.tags) {
		sel = f.tags[f.tagView[prev]].Name
	}
	f.tagView = f.tagView[:0]
	q := strings.ToLower(strings.TrimSpace(f.tagQuery))
	for i, t := range f.tags {
		if q == "" || strings.Contains(strings.ToLower(t.Name), q) {
			f.tagView = append(f.tagView, i)
		}
	}
	// Keep the cursor on the same tag when it survived the filter.
	pos := 0
	if sel != "" {
		for vi, ti := range f.tagView {
			if strings.EqualFold(f.tags[ti].Name, sel) {
				pos = vi
				break
			}
		}
	}
	if f.cur >= 0 && f.cur < len(f.rows) {
		f.rows[f.cur].tagPos = pos
	}
	f.tagOff = 0
}

// tagCursor returns the index into f.tags of the highlighted tag.
func (f *metaForm) tagCursor() int {
	vi := f.tagCursorView()
	if vi < 0 || vi >= len(f.tagView) {
		return 0
	}
	return f.tagView[vi]
}

// tagCursorView is the position within the visible list.
func (f *metaForm) tagCursorView() int {
	if f.cur < 0 || f.cur >= len(f.rows) {
		return 0
	}
	vi := f.rows[f.cur].tagPos
	if vi < 0 || vi >= len(f.tagView) {
		return 0
	}
	return vi
}

// clampTagOff keeps the first visible line such that the cursor is on screen.
func clampTagOff(off, cur, n, rows int) int {
	if n == 0 || rows <= 0 {
		return 0
	}
	if cur < off {
		off = cur
	}
	if cur >= off+rows {
		off = cur - rows + 1
	}
	if off < 0 {
		off = 0
	}
	if max := n - rows; off > max {
		off = max
	}
	if off < 0 {
		off = 0
	}
	return off
}

// tagPickerRows is how many checklist lines fit on screen. The picker is
// bounded so a table of hundreds of tags cannot push the rest of the form off.
func (m *Model) tagPickerRows() int {
	if m.height < 20 {
		return 4
	}
	return 8
}

// viewMetaForm renders the inline editor.
// viewMetaForm renders the built-in editor as a floating panel over the
// browser, so it reads as a dialog rather than a separate screen.
func (m *Model) viewMetaForm() string {
	f := m.metaForm
	if f == nil {
		return "metadata"
	}
	w, h := m.width, m.height
	if w < 30 || h < 10 {
		return "terminal too small for the metadata form"
	}

	boxW := m.modalWidth(w - 4)
	// The form sizes itself to its content: two rows per field, and the boxes
	// grow with the text they hold.
	boxH := m.modalHeight(m.metaFormHeight())

	all, starts, ends := m.layoutMetaForm(boxW - 2)
	// two border rows, separator, footer
	avail := max(boxH-4, 1)
	// Keep the cursor row fully on screen.
	cur := f.cur
	if cur >= 0 && cur < len(starts) {
		if starts[cur] < f.off {
			f.off = starts[cur]
		}
		if ends[cur] > f.off+avail {
			f.off = ends[cur] - avail
		}
	}
	if maxOff := len(all) - avail; f.off > maxOff {
		f.off = maxInt(maxOff, 0)
	}
	if f.off < 0 {
		f.off = 0
	}

	var body []string
	if f.off < len(all) {
		body = append(body, all[f.off:minInt(f.off+avail, len(all))]...)
	}
	for len(body) < avail {
		body = append(body, "")
	}
	content := strings.Join(body, "\n") + "\n" +
		stFaint.Render(strings.Repeat("─", maxInt(boxW-2, 0))) + "\n" +
		stDim.Render(m.metaFormFooter())
	box := panel("edit metadata  "+app.Truncate(f.name, maxInt(boxW-22, 8)), content, boxW, boxH, true)
	return overlayCard(box, dimScreen(m.viewBrowser()), w, h)
}

// metaFormHeight is how many body rows the form wants at the current width.
func (m *Model) metaFormHeight() int {
	w := m.modalWidth(m.width-4) - 2
	_, _, ends := m.layoutMetaForm(w)
	if len(ends) == 0 {
		return 6
	}
	return ends[len(ends)-1] + 1
}

// metaFormFooter is the help/cursor status line.
func (m *Model) metaFormFooter() string {
	f := m.metaForm
	if f.status != "" {
		if f.statusK {
			return stErr.Render(f.status)
		}
		return stAccent.Render(f.status)
	}
	hint := "tab move  enter save  esc cancel"
	if f.cur >= 0 && f.cur < len(f.rows) {
		switch f.rows[f.cur].kind {
		case rowTags:
			hint = "tab move  ←/→ pick  space toggle  ctrl+s save  esc cancel"
			if len(f.tagBad) > 0 {
				for i, bad := range f.tagBad {
					if bad && i < len(f.tagOn) && f.tagOn[i] {
						hint = "! " + f.tags[i].Name + " is not in the tag table: untick it or add it in settings"
						break
					}
				}
			}
		case rowAbstract, rowComment, rowExtra:
			hint = "tab move  enter newline  ctrl+s save  esc cancel"
		case rowNewTag:
			hint = "enter adds the tag  tab move  ctrl+s save  esc cancel"
		case rowNewField:
			hint = "enter adds a field written as name=value  ctrl+s save"
		}
	}
	return stDim.Render(hint)
}

// layoutMetaForm renders every row into a flat list of lines and reports the
// line span of each row, so the view can scroll by row.
func (m *Model) layoutMetaForm(w int) (lines []string, starts, ends []int) {
	f := m.metaForm
	inner := w - 4
	for i, r := range f.rows {
		cur := i == f.cur
		mark := "  "
		if cur {
			mark = stAccent.Render("› ")
		}
		start := len(lines)
		switch r.kind {
		case rowAction:
			lines = append(lines, mark+stKey.Render("[ save ]")+stDim.Render("   (enter or ctrl+s)"),
				"")
		case rowTags:
			lines = append(lines, mark+stFaint.Render(r.label))
			lines = append(lines, m.metaTagLines(r, inner, cur)...)
			lines = append(lines, "")
		case rowAbstract, rowComment, rowExtra:
			lines = append(lines, mark+stFaint.Render(r.label))
			area := r.area
			area.SetWidth(maxInt(inner-4, 10))
			fitAreaHeight(&area, area.Value(), maxInt(inner-4, 10), areaBudget(m.height))
			for l := range strings.SplitSeq(area.View(), "\n") {
				lines = append(lines, "    "+l)
			}
			lines = append(lines, "")
		default:
			lines = append(lines, mark+stFaint.Render(r.label))
			lines = append(lines, "    "+r.input.View())
			lines = append(lines, "")
		}
		starts = append(starts, start)
		ends = append(ends, len(lines))
	}
	return lines, starts, ends
}

// metaTagLines renders the tag picker: a filter line, then a scrolling window
// over the visible tags.
//
// It is a list rather than a wall of chips because a real collection has
// hundreds of tags: wrapping them all would push every other field off the
// screen, and there would be no way to reach one whose name you do not want to
// type in full. Bounding the window means the form has the same height whatever
// the table size, and typing narrows it.
func (m *Model) metaTagLines(r metaRow, w int, cur bool) []string {
	f := m.metaForm
	if len(f.tags) == 0 {
		return []string{"    " + stDim.Render("no tags in the table yet — add one below")}
	}

	// The filter line. It is always shown, so the picker's shape does not change
	// when it is focused, and it doubles as the indication of what is filtered.
	input := f.tagInput
	input.Width = maxInt(w-8, 8)
	switch {
	case f.tagTyping && cur:
		lines := []string{"    " + input.View()}
		return append(lines, m.tagWindow(r, w, cur)...)
	case f.tagQuery != "":
		return append([]string{"    " + stAccent.Render("/") + " " + stDim.Render(f.tagQuery)}, m.tagWindow(r, w, cur)...)
	}
	out := []string{"    " + stDim.Render(fmt.Sprintf("%d tags — / to filter", len(f.tags)))}
	return append(out, m.tagWindow(r, w, cur)...)
}

// tagWindow renders the visible slice of the picker, marking the cursor and the
// entries the tag table does not know.
func (m *Model) tagWindow(r metaRow, w int, cur bool) []string {
	f := m.metaForm
	n := len(f.tagView)
	rows := m.tagPickerRows()
	if n == 0 {
		return []string{"    " + stDim.Render("no tag matches "+strconv.Quote(f.tagQuery))}
	}
	off := min(r.tagPos, f.tagOff)
	if r.tagPos >= off+rows {
		off = r.tagPos - rows + 1
	}
	off = clampTagOff(off, r.tagPos, n, rows)
	end := minInt(off+rows, n)

	out := make([]string, 0, end-off)
	for vi := off; vi < end; vi++ {
		ti := f.tagView[vi]
		t := f.tags[ti]
		box, mark := "[-]", "  "
		if f.tagOn[ti] {
			box = "[x]"
		}
		if vi == r.tagPos && cur {
			mark = "▸ "
		}
		bad := ti < len(f.tagBad) && f.tagBad[ti]
		label := mark + box + " " + t.Name
		if bad {
			label = "▸ [!] " + t.Name
			if vi != r.tagPos || !cur {
				label = "  [!] " + t.Name
			}
		}
		// A count is what makes the list scannable: it says at a glance which
		// tags this collection actually uses.
		if c := f.tagCount[f.tags[ti].Name]; c > 0 {
			label += stDim.Render(fmt.Sprintf("  %d", c))
		}
		switch {
		case bad:
			out = append(out, "    "+stErr.Render(app.Truncate(label, w-4)))
		case vi == r.tagPos && cur:
			out = append(out, "    "+stCursor.Render(fit(label, maxInt(w-4, 4))))
		case f.tagOn[ti]:
			out = append(out, "    "+stTag.Render(app.Truncate(label, w-4)))
		default:
			out = append(out, "    "+stDim.Render(app.Truncate(label, w-4)))
		}
	}
	if off > 0 {
		out = append([]string{"    " + stFaint.Render(fmt.Sprintf("↑ %d more", off))}, out...)
	}
	if end < n {
		out = append(out, "    "+stFaint.Render(fmt.Sprintf("↓ %d more", n-end)))
	}
	return out
}

// metaFormAddField appends a new extra-field row and focuses it, so a field
// that did not exist yet can be created without leaving the form.
func (m *Model) metaFormAddField(key, value string) {
	f := m.metaForm
	// Replace an existing row with the same key rather than duplicating it.
	for i := range f.rows {
		if f.rows[i].kind == rowExtra && strings.EqualFold(f.rows[i].label, key) {
			f.rows[i].area.SetValue(value)
			f.setStatus("updated field " + key)
			return
		}
	}
	row := metaRow{label: key, kind: rowExtra, area: newArea(value)}
	// Insert before the tag block so the order stays title → fields → tags.
	at := len(f.rows)
	for i, r := range f.rows {
		if r.kind == rowTags {
			at = i
			break
		}
	}
	f.rows = append(f.rows, metaRow{})
	copy(f.rows[at+1:], f.rows[at:])
	f.rows[at] = row
	f.cur = at
	m.focusMetaRow()
	f.setStatus("added field " + key)
}
