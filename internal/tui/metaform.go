package tui

import (
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

	tags     []meta.Tag // global table, sorted
	tagOn    []bool     // selection matching tags
	tagCount map[string]int

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
	f.rows = append(f.rows, metaRow{label: "Tags", kind: rowTags})
	f.rows = append(f.rows, metaRow{label: "Add tag", kind: rowNewTag, input: newInput("", "type a name and press enter")})
	f.rows = append(f.rows, metaRow{label: "New field", kind: rowNewField,
		input: newInput("", "name=value")})
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
	c := h / 4
	if c < 3 {
		c = 3
	}
	if c > maxAreaHeight {
		c = maxAreaHeight
	}
	return c
}

// fitAreaHeight grows a text box to fit the value wrapped at the given width,
// up to maxAreaHeight lines, so long paragraphs are readable in place.
func fitAreaHeight(ta *textarea.Model, value string, width, cap int) {
	if width < 10 {
		width = 10
	}
	n := 0
	for _, para := range strings.Split(value, "\n") {
		w := lipgloss.Width(para)
		lines := (w + width - 1) / width
		if lines < 1 {
			lines = 1
		}
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
		for i := 0; i < n; i++ {
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
}

// --- keys -----------------------------------------------------------------

func (m *Model) handleMetaFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f := m.metaForm
	if f == nil {
		m.mode = modeBrowse
		return m, nil
	}
	key := msg.String()
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
		return m.metaTagsKey(key)
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

// metaTagsKey handles the checklist row.
func (m *Model) metaTagsKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case " ", "x":
		m.metaFormToggleTag()
		return m, nil
	case "j", "right", "l":
		m.metaFormMoveTag(1)
		return m, nil
	case "k", "left", "h":
		m.metaFormMoveTag(-1)
		return m, nil
	}
	return m, nil
}

// tagCursor returns the index of the highlighted tag.
func (f *metaForm) tagCursor() int {
	if f.cur < 0 || f.cur >= len(f.rows) {
		return 0
	}
	return f.rows[f.cur].tagsAt(f.tags)
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
	for i, on := range f.tagOn {
		if on {
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
	if added {
		f.setStatus("added tag " + canonical)
	} else {
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
	c := r.tagPos + delta
	if len(f.tags) == 0 {
		return
	}
	if c < 0 {
		c = len(f.tags) - 1
	}
	if c >= len(f.tags) {
		c = 0
	}
	r.tagPos = c
}

// tagCursor returns the highlighted index of the checklist row.
func (r metaRow) tagsAt(tags []meta.Tag) int {
	if r.tagPos < 0 || r.tagPos >= len(tags) {
		return 0
	}
	return r.tagPos
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
	avail := boxH - 4 // two border rows, separator, footer
	if avail < 1 {
		avail = 1
	}
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
			for _, l := range strings.Split(area.View(), "\n") {
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

// metaTagLines renders the tag checklist, wrapped to the pane width.
func (m *Model) metaTagLines(r metaRow, w int, cur bool) []string {
	f := m.metaForm
	if len(f.tags) == 0 {
		return []string{"    " + stDim.Render("no tags in the table yet — add one below")}
	}
	var chips []string
	pos := 0
	cursor := r.tagPos
	for i, t := range f.tags {
		box := "[-]"
		if f.tagOn[i] {
			box = "[x]"
		}
		label := box + " " + t.Name
		if cur && i == cursor {
			chips = append(chips, stCursor.Render("▸"+label))
		} else if f.tagOn[i] {
			chips = append(chips, stTag.Render(label))
		} else {
			chips = append(chips, stDim.Render(label))
		}
		pos += len(label) + 2
	}
	// Wrap chips into lines of the available width.
	var out []string
	line := "    "
	width := 0
	for _, c := range chips {
		cw := lipgloss.Width(c)
		if width > 0 && width+cw+2 > w {
			out = append(out, line)
			line = "    "
			width = 0
		}
		line += c + "  "
		width += cw + 2
	}
	if strings.TrimSpace(line) != "" {
		out = append(out, line)
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
