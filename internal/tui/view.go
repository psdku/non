package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"non/internal/app"
	"non/internal/meta"
)

const (
	minListWidth = 24
	minInfoWidth = 24
)

// View implements tea.Model.
func (m *Model) View() string {
	if !m.ready {
		return "loading…"
	}
	if m.quitting {
		return ""
	}
	if m.mode == modeHelp {
		return m.clampScreen(m.viewHelp())
	}
	if m.mode == modeSettings {
		return m.clampScreen(m.viewModal(m.viewSettings()))
	}
	if m.mode == modeConfirm {
		return m.clampScreen(m.viewConfirm())
	}
	if m.mode == modeMetaForm {
		return m.clampScreen(m.viewMetaForm())
	}

	return m.viewBrowser()
}

// viewBrowser renders the browser: two bordered panes between a one-line
// header and a status bar, the layout yazi uses for its manager.
func (m *Model) viewBrowser() string {
	w, h := m.width, m.height
	if w < 20 || h < 6 {
		return "terminal too small"
	}

	header := m.viewHeader(w)
	bar := m.viewFooter(w)
	bodyH := h - 2

	leftW := m.leftWidth(w)
	rightW := w - leftW

	left := m.viewList(leftW, bodyH)
	right := m.viewInfo(rightW, bodyH)
	body := joinRows(left, right)
	return header + "\n" + body + "\n" + bar
}

// leftWidth splits the width between the list and the details pane. The list
// gets the larger share (yazi's ratio is 4 list : 3 preview), unless the
// terminal is narrow enough that the details pane would be unusable.
func (m *Model) leftWidth(w int) int {
	l := w * 4 / 7
	if l < minListWidth {
		l = minListWidth
	}
	if w-l < minInfoWidth {
		l = w - minInfoWidth
	}
	if l < 10 {
		l = 10
	}
	if l > w {
		l = w
	}
	return l
}

// viewHeader is the top line: the app name, the current directory, and the
// search box when searching.
func (m *Model) viewHeader(w int) string {
	dir := m.dir
	if home, err := homeDir(); err == nil && strings.HasPrefix(dir, home) {
		dir = "~" + strings.TrimPrefix(dir, home)
	}
	if m.mode == modeFilter {
		// The error goes on the right of the prompt line, truncated rather than
		// wrapped, so the header stays one row while a long message is typed.
		line := m.fin.View()
		msg, style := "", stErr
		if m.filterErr != nil {
			msg = m.filterErr.Error()
		} else if unknown := m.filterUnknown(); len(unknown) > 0 {
			msg, style = "no such tag: "+strings.Join(unknown, ", "), stTag
		}
		if msg != "" {
			used := lipgloss.Width(line) + 2
			if room := w - used; room > 8 {
				line += stDim.Render("  ") + style.Render(app.Truncate(msg, room))
			}
		}
		return fit(line, w)
	}
	if m.mode == modeSearch {
		line := stKey.Render("/") + m.search.View()
		if m.metaUI {
			line += stTag.Render("  +meta")
		}
		return fit(line, w)
	}
	line := stTitle.Render("non") + " " + stCwd.Render(dir)
	// The active filter is shown as a bracketed expression, so it is obvious
	// that the list is a subset and why.
	if m.filterActive() {
		expr := m.filterTree.String()
		if m.filterErr != nil {
			expr = m.filterText
		}
		line += " " + stAccent.Render("[ "+expr+" ]")
	}
	if m.quat != "" {
		line += stDim.Render("  ") + stTag.Render("/"+m.quat)
	}
	if m.metaUI && m.quat != "" {
		line += stTag.Render(" +meta")
	}
	return fit(line, w)
}

// viewFooter is the status bar. It follows yazi's shape: a mode block on the
// left, then the paper count, then the key hints on the right.
func (m *Model) viewFooter(w int) string {
	var blocks []string

	mode := "NORMAL"
	switch m.mode {
	case modeSearch:
		mode = "SEARCH"
	case modeActions:
		mode = "ACTION"
	case modeOpener:
		mode = "OPEN"
	case modeRename:
		mode = "RENAME"
	case modeInspect:
		mode = "INSPECT"
	case modeFilter:
		mode = "FILTER"
	case modeConfirm:
		mode = "CONFIRM"
	}
	blocks = append(blocks, stBarMode.Render(" "+mode+" "))

	// The status message replaces the count: it is the most recent thing the
	// user did, and yazi puts progress/status in the same slot.
	switch {
	case m.status != "" && m.statusK:
		blocks = append(blocks, stErr.Render(m.status))
	case m.status != "":
		blocks = append(blocks, stOK.Render(m.status))
	default:
		blocks = append(blocks, stDim.Render(m.statusLine()))
	}
	// A filter that is in force but produced no message still needs to show
	// how much is hidden, or the list looks mysteriously short.
	if m.status == "" && m.filterActive() {
		blocks = append(blocks, stAccent.Render(fmt.Sprintf("filter %d/%d", len(m.visible), len(m.all))))
	}
	if p := m.current(); p != nil && p.Meta != nil && len(p.Meta.Tags) > 0 {
		blocks = append(blocks, stTag.Render(tagSummary(p.Meta.Tags)))
	}

	left := strings.Join(blocks, stFaint.Render("  "))
	hints := m.footerHints()
	gap := w - lipgloss.Width(left) - lipgloss.Width(hints)
	if gap < 1 {
		return fit(left, w)
	}
	return left + strings.Repeat(" ", gap) + hints
}

// footerHints is the key hint block on the right of the status bar.
func (m *Model) footerHints() string {
	hint := func(k, d string) string { return stKey.Render(k) + " " + stFaint.Render(d) }
	switch m.mode {
	case modeSearch:
		return hint("enter", "keep") + stFaint.Render("  ") + hint("esc", "clear") +
			stFaint.Render("  ") + hint("tab", "metadata")
	case modeActions, modeOpener:
		return hint("j/k", "move") + stFaint.Render("  ") + hint("enter", "run") +
			stFaint.Render("  ") + hint("esc", "cancel")
	case modeRename:
		return hint("enter", "confirm") + stFaint.Render("  ") + hint("esc", "cancel")
	case modeInspect:
		return hint("esc", "close")
	case modeFilter:
		return hint("enter", "keep") + stFaint.Render("  ") + hint("esc", "clear")
	case modeConfirm:
		return hint("y/enter", "accept") + stFaint.Render("  ") + hint("n/esc", "decline")
	case modeSettings, modeMetaForm:
		return ""
	}
	return hint("?", "help") + stFaint.Render("  ") + hint("f", "filter") +
		stFaint.Render("  ") + hint("/", "search") +
		stFaint.Render("  ") + hint("enter", "actions") + stFaint.Render("  ") +
		hint("s", "settings") + stFaint.Render("  ") + hint("q", "quit")
}

// viewList renders the file list pane: a bordered box titled with the number of
// papers, the current line in reverse video, and any menu overlaid as a
// dropdown beneath the cursor.
func (m *Model) viewList(w, h int) string {
	innerW, innerH := w-2, h-2
	if innerW < 1 || innerH < 1 {
		return panel("", "", w, h, true)
	}
	lines := make([]string, innerH)

	empty := len(m.visible) == 0
	switch {
	case empty && len(m.all) == 0:
		lines[0] = stDim.Render(" no *.pdf files here")
	case empty:
		lines[0] = stDim.Render(" no matching papers")
	default:
		end := minInt(m.offset+innerH, len(m.visible))
		for i := m.offset; i < end; i++ {
			at := i - m.offset
			if at < 0 || at >= innerH {
				continue
			}
			lines[at] = m.listRow(i, innerW)
		}
	}

	if !empty {
		if m.mode == modeActions && len(m.actions) > 0 {
			lines = overlayMenu(lines, m.actionMenu(innerW), m.cursor-m.offset, innerH)
		}
		if m.mode == modeOpener && len(m.openerList) > 0 {
			lines = overlayMenu(lines, m.openerMenu(innerW), m.cursor-m.offset, innerH)
		}
	}
	if m.mode == modeRename {
		at := m.cursor - m.offset + 1
		if at < 0 {
			at = 0
		}
		if at >= innerH {
			at = innerH - 1
		}
		lines[at] = fit(" "+m.input.View(), innerW)
	}

	title := fmt.Sprintf("%d/%d", len(m.visible), len(m.all))
	if len(m.visible) == len(m.all) {
		title = fmt.Sprintf("%d papers", len(m.all))
	}
	return panel(title, strings.Join(lines, "\n"), w, h, m.listFocused())
}

// overlayMenu draws a dropdown menu starting under row `from`, moving it up
// when there is not enough room below so every entry stays visible.
func overlayMenu(lines, menu []string, from, h int) []string {
	if len(menu) == 0 || h <= 0 {
		return lines
	}
	top := from + 1
	if top+len(menu) > h {
		top = h - len(menu)
	}
	if top < 0 {
		top = 0
	}
	for i, row := range menu {
		at := top + i
		if at >= 0 && at < h {
			lines[at] = row
		}
	}
	return lines
}

// listFocused reports whether the list pane owns the keyboard.
func (m *Model) listFocused() bool {
	switch m.mode {
	case modeBrowse, modeSearch, modeActions, modeOpener, modeRename:
		return true
	}
	return false
}

// maxRowTags is how many tags a single-line display shows before summarizing.
// A paper with eight tags would otherwise push its own name out of the row.
const maxRowTags = 3

// tagSummary renders tags for a single-line display: the first three, with a
// "+" on the last when there are more. It is deliberately not a count — the
// list is for scanning, and the details pane has the full list.
func tagSummary(tags []string) string {
	switch {
	case len(tags) == 0:
		return ""
	case len(tags) <= maxRowTags:
		return strings.Join(tags, " ")
	}
	return strings.Join(tags[:maxRowTags], " ") + "+"
}

// listRow renders one file: the name on the left, its tags flush right.
//
// The title and author are deliberately absent: the details pane already shows
// them for the selection, and repeating them on every row turned the list into
// two competing columns of text. What the row has to answer is "which file is
// this, and what is it tagged", which the name and tags do exactly.
func (m *Model) listRow(i, w int) string {
	p := m.all[m.visible[i]]
	cur := i == m.cursor

	tags := ""
	if p.Meta != nil {
		tags = tagSummary(p.Meta.Tags)
	}

	room := w - 2 // one cell of padding on each side
	if room < 4 {
		room = 4
	}
	// Tags are the only thing that may be dropped, and only when there is no
	// room for both; the name always wins, because it identifies the row.
	tagW := 0
	if tags != "" {
		tagW = lipgloss.Width(tags)
		if tagW > room/2 {
			tagW = maxInt(room/2, 1)
		}
		tags = app.Truncate(tags, tagW)
		tagW = lipgloss.Width(tags)
	}
	name := app.Truncate(p.Name, maxInt(room-tagW-1, 4))
	gap := room - lipgloss.Width(name) - tagW
	if gap < 1 {
		gap = 1
	}

	if cur {
		// One reverse bar for the whole row: no coloured text inside, because
		// a foreground colour would fight the reversed background.
		return stCursor.Render(fit(" "+name+strings.Repeat(" ", gap)+tags, w))
	}
	out := stFaint.Render(" " + name + strings.Repeat(" ", gap))
	if tagW > 0 {
		out += stTag.Render(tags)
	}
	return pad(out, w)
}

// openerMenu renders the tool picker dropdown. Rows are laid out as plain
// text first so the right border always lines up, then styled.
func (m *Model) openerMenu(w int) []string {
	const head = "open with"
	width := lipgloss.Width(head) + 2
	for _, t := range m.openerList {
		if l := lipgloss.Width(t.Name) + lipgloss.Width(t.Type) + 6; l > width {
			width = l
		}
	}
	if width > w {
		width = w
	}
	if width < len(head)+2 {
		width = len(head) + 2
	}
	return menuRows(head, width, len(m.openerList), m.openerCur, func(i int) (string, string) {
		t := m.openerList[i]
		return t.Name, t.Type
	})
}

func (m *Model) actionMenu(w int) []string {
	const label = "actions"
	width := lipgloss.Width(label) + 4
	for _, a := range m.actions {
		if l := lipgloss.Width(a.name) + lipgloss.Width(a.key) + 6; l > width {
			width = l
		}
	}
	if width > w {
		width = w
	}
	if width < 14 {
		width = 14
	}
	return menuRows(label, width, len(m.actions), m.actCur, func(i int) (string, string) {
		a := m.actions[i]
		key := a.key
		if key == "esc" {
			key = ""
		}
		return a.name, key
	})
}

// menuRows renders a dropdown: a titled top rule, one row per entry with the
// text left and its shortcut right, and a closing rule. Every row is built to
// exactly width cells before the side rules are added, so the right edge always
// lines up whatever the row contains.
func menuRows(label string, width, n, cur int, entry func(i int) (name, key string)) []string {
	rule := lipgloss.NewStyle().Foreground(colBorder).Render
	out := make([]string, 0, n+2)

	head := " " + label + " "
	if lipgloss.Width(head) < width-1 {
		head += strings.Repeat("─", width-1-lipgloss.Width(head))
	}
	out = append(out, rule("╭")+stDim.Render(app.Truncate(head, width-1))+rule("╮"))

	for i := 0; i < n; i++ {
		name, key := entry(i)
		keyW := lipgloss.Width(key)
		// width-2 for the side rules, 1 of left margin, 1 of gap before the key.
		room := width - 2 - 1
		if keyW > 0 {
			room -= keyW + 1
		}
		text := app.Truncate(name, maxInt(room, 4))
		gap := maxInt(room-lipgloss.Width(text), 1)
		if i == cur {
			row := fit(" "+text+strings.Repeat(" ", gap)+key, width-2)
			out = append(out, rule("│")+stCursor.Render(row)+rule("│"))
			continue
		}
		row := stFaint.Render(" "+text+strings.Repeat(" ", gap)) + stKey.Render(key)
		out = append(out, rule("│")+fit(row, width-2)+rule("│"))
	}
	out = append(out, rule("╰"+strings.Repeat("─", width-2)+"╯"))
	return out
}

// viewInfo renders the details pane, bordered and titled with the file name.
func (m *Model) viewInfo(w, h int) string {
	p := m.current()
	inner := w - 2
	if inner < 1 || h-2 < 1 {
		return panel("", "", w, h, false)
	}

	if p == nil {
		return panel("details", stDim.Render(" no selection"), w, h, false)
	}
	if m.mode == modeInspect {
		return m.viewInspect(w, h)
	}

	title := "details"
	focused := false

	e := p.Meta
	if e == nil || (e.Title == "" && e.Abstract == "") && !m.hasAny(e) {
		body := stDim.Render("no metadata yet") + "\n" +
			stDim.Render("press m to fill it in")
		return panel(title, body, w, h, focused)
	}

	// wrapInto appends text wrapped to the pane width, one entry per line, so
	// that a long paragraph never inflates the view height.
	var lines []string
	wrapInto := func(text string) {
		for _, l := range wrapText(text, inner-2) {
			lines = append(lines, " "+l)
		}
	}
	blank := func() { lines = append(lines, "") }

	lines = append(lines, " "+stTitle.Render(app.Truncate(m.titleOf(p), inner-2)))
	if e.Author != "" {
		wrapInto(stFaint.Render(e.Author))
	}
	if e.Title == "" {
		wrapInto(stErr.Render("title missing — press m"))
	}
	if len(e.Tags) > 0 {
		wrapInto(stTag.Render("#" + strings.Join(e.Tags, " #")))
	}

	if e.Comment != "" {
		blank()
		lines = append(lines, " "+stCwd.Render("Comment"))
		wrapInto(e.Comment)
	}
	if e.Abstract != "" {
		blank()
		lines = append(lines, " "+stCwd.Render("Abstract"))
		wrapInto(e.Abstract)
	}
	if extra := extraFields(e); len(extra) > 0 {
		blank()
		for _, kv := range extra {
			wrapInto(stDim.Render(kv[0]+": ") + kv[1])
		}
	}
	if e.Auto {
		blank()
		lines = append(lines, " "+stDim.Render("autogenerated — press m to edit"))
	}

	// Scroll the details pane with J/K.
	if m.details > 0 {
		if m.details >= len(lines) {
			m.details = maxInt(len(lines)-1, 0)
		}
		lines = lines[m.details:]
	}
	return panel(title, strings.Join(lines, "\n"), w, h, focused)
}

// hasAny reports whether the entry carries any content at all.
func (m *Model) hasAny(e *meta.Entry) bool {
	if e == nil {
		return false
	}
	return e.Title != "" || e.Author != "" || len(e.Tags) > 0 ||
		e.Abstract != "" || e.Comment != "" || len(e.Fields) > 0
}

func (m *Model) viewInspect(w, h int) string {
	inner := w - 2
	if inner < 1 || h-2 < 1 {
		return panel("inspect", "", w, h, false)
	}
	var lines []string
	for l := range strings.SplitSeq(m.inspect.String(), "\n") {
		lines = append(lines, wrapText(l, inner-2)...)
	}
	// Scroll the pane with J/K.
	if m.details > 0 {
		if m.details >= len(lines) {
			m.details = maxInt(len(lines)-1, 0)
		}
		lines = lines[m.details:]
	}
	return panel("inspect", strings.Join(lines, "\n"), w, h, false)
}

func (m *Model) viewHelp() string {
	rows := [][2]string{
		{"j / ↓ / ctrl+n", "move down"},
		{"k / ↑ / ctrl+p", "move up"},
		{"ctrl+f / ctrl+b", "page down / up"},
		{"g / G", "first / last"},
		{"f", "filter by tag expression: LLM && !DB"},
		{"/", "fuzzy search (enter keeps, esc clears)"},
		{"tab", "toggle searching metadata"},
		{"esc", "clear the search, then the filter"},
		{"enter", "action menu for the selected paper"},
		{"o", "open with the default tool"},
		{"O", "open with… (choose a tool)"},
		{"r", "rename the file"},
		{"m / ctrl+e", "edit metadata ($EDITOR or the built-in form)"},
		{"s", "settings: tools, tags, editing mode"},
		{"e", "re-extract metadata from the PDF"},
		{"i", "inspect file and PDF info"},
		{"J / K", "scroll the info pane"},
		{"R", "rescan the directory"},
		{"q / ctrl+c", "quit"},
	}

	w, h := m.width, m.height
	if w < 20 || h < 6 {
		return "terminal too small"
	}
	boxW := m.modalWidth(w - 4)
	// Key column, gap, description.
	keyW := 18
	var body []string
	for _, r := range rows {
		body = append(body, " "+stKey.Render(padRight(r[0], keyW))+
			stFaint.Render(app.Truncate(r[1], maxInt(boxW-keyW-4, 8))))
	}
	body = append(body, "",
		" "+stDim.Render(app.Truncate("metadata lives in "+meta.StoreFile, boxW-4)),
		" "+stDim.Render("press s for settings, esc to close"))

	boxH := minInt(len(body)+2, h-2)
	box := panel("keys", strings.Join(body, "\n"), boxW, boxH, true)
	return overlayCard(box, dimScreen(m.viewBrowser()), w, h)
}

// extraFields returns the non standard fields as key/value pairs.
func extraFields(e *meta.Entry) [][2]string {
	if len(e.Fields) == 0 {
		return nil
	}
	keys := make([]string, 0, len(e.Fields))
	for k := range e.Fields {
		keys = append(keys, k)
	}
	sortStrings(keys)
	out := make([][2]string, 0, len(keys))
	for _, k := range keys {
		v := strings.TrimSpace(e.FieldString(k))
		if v == "" {
			continue
		}
		out = append(out, [2]string{k, collapse(v)})
	}
	return out
}

// --- small helpers --------------------------------------------------------

// pad renders s in a field of exactly w cells: it is truncated when too long
// and space-padded when too short. Negative widths are clamped, so a very
// narrow terminal cannot produce a negative Repeat count.
func pad(s string, w int) string {
	if w <= 0 {
		return ""
	}
	sw := lipgloss.Width(s)
	if sw >= w {
		return ansi.Truncate(s, w, "")
	}
	return s + strings.Repeat(" ", w-sw)
}

func padRight(s string, w int) string { return pad(s, w) }

// fit pads or truncates a (possibly styled) string to exactly w cells.
//
// It pads as well as truncates: a panel row must occupy its whole column, or
// the border that follows it would slide left on short rows.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	sw := lipgloss.Width(s)
	if sw > w {
		return ansi.Truncate(s, w, "…")
	}
	if sw == w {
		return s
	}
	return s + strings.Repeat(" ", w-sw)
}

func trimToHeight(lines []string, h int) []string {
	if h <= 0 {
		return nil
	}
	if len(lines) > h {
		return lines[:h]
	}
	return lines
}

// wrap hard-wraps styled-free text to width.

func wrapText(s string, w int) []string {
	if w < 4 {
		w = 4
	}
	var out []string
	for para := range strings.SplitSeq(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			out = append(out, "")
			continue
		}
		words := strings.Fields(para)
		line := ""
		for _, word := range words {
			switch {
			case line == "":
				line = word
			case len([]rune(line))+1+len([]rune(word)) <= w:
				line += " " + word
			default:
				out = append(out, line)
				line = word
			}
		}
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func homeDir() (string, error) { return osUserHomeDir() }

var _ = fmt.Sprintf

// --- settings modal -------------------------------------------------------

// modalChrome is the number of rows a modal spends on anything that is not the
// rows it is showing: top border, separator, hint, bottom border.
const modalChrome = 4

// modalMinRows is the smallest body a list modal will show. Below this a list
// is useless, so the box grows to it and the page scrolls instead of shrinking.
const modalMinRows = 3

// modalWidth is the box width of a modal. The root settings page is a narrow
// dialog; a list page (tools, tags) earns more width because its rows are
// tables.
func (m *Model) modalWidth(w int) int {
	max := 76
	if m.settings != nil {
		switch m.settings.page {
		case pageTools, pageTags, pageToolForm, pageTagForm:
			max = 96
		}
	}
	if w > max {
		return max
	}
	if w < 20 {
		return w
	}
	return w
}

// modalHeight sizes a modal.
//
// rows is how many body rows the caller wants (0 = ask the settings page). The
// box grows to fit them, but never past what the terminal can show: it keeps a
// row of browser visible above and below, and never exceeds 80% of the height,
// so a dialog stays a dialog on a tall screen instead of becoming full-screen.
func (m *Model) modalHeight(rows int) int {
	if rows <= 0 {
		rows = m.settingsBodyRows()
	}
	h := rows + modalChrome
	// Leave the header and the status bar visible around the box.
	if limit := m.height - 2; h > limit {
		h = limit
	}
	// Never take more than four fifths of the terminal.
	if limit := m.height * 4 / 5; h > limit {
		h = limit
	}
	if h < modalChrome+modalMinRows {
		h = modalChrome + modalMinRows
	}
	if h > m.height {
		h = m.height
	}
	return h
}

// modalBodyHeight is how many rows of content a modal box can show: the box
// minus its border, its separator and its hint line.
func (m *Model) modalBodyHeight() int {
	h := m.modalHeight(0) - modalChrome
	if h < 1 {
		return 1
	}
	return h
}

// settingsBodyRows is how many rows the current page wants, including the one
// row of help under the selected setting.
func (m *Model) settingsBodyRows() int {
	s := m.settings
	switch s.page {
	case pageRoot:
		// One row per setting. The help line is a fixed row of the box, not
		// part of the list, so nothing shifts as the cursor moves.
		return len(s.root) + 1
	case pageTools:
		// Column head, one row per tool, a spacer.
		return maxInt(len(s.tools), 1) + 2
	case pageTags:
		return maxInt(len(s.tags), 1) + 2
	case pageToolForm, pageTagForm:
		// Each field is a label row plus its input row.
		return len(s.form)*2 + 1
	}
	return 6
}

// viewSettings renders the settings modal.
// viewSettings builds the settings modal. The modal is a floating box: the
// browser stays behind it, dimmed, and the box is centred with its own border.
func (m *Model) viewSettings() string {
	s := m.settings
	if s == nil {
		return "settings"
	}
	w := m.modalWidth(m.width - 4)
	if w < 20 {
		return "terminal too small"
	}
	hint := ""
	switch s.page {
	case pageRoot:
		hint = "j/k move  space toggle  h/l change  esc close"
	case pageTools:
		hint = "j/k move  enter edit  a add  d delete  s default  esc back"
	case pageToolForm:
		hint = "tab next  enter next/save  ctrl+s save  esc back"
	case pageTags:
		hint = "j/k move  enter edit  a add  d delete  esc back"
	case pageTagForm:
		hint = "tab next  enter next/save  ctrl+s save  esc back"
	}

	// The body height is whatever is left inside the box after the title row,
	// the hint, and the optional status line.
	h := m.modalHeight(0)
	innerH := h - 2
	var body []string
	switch s.page {
	case pageRoot:
		body = m.settingsRootLines(w - 2)
	case pageTools:
		body = m.settingsToolLines(w - 2)
	case pageToolForm:
		body = m.settingsFormLines(w-2, "tool")
	case pageTags:
		body = m.settingsTagLines(w - 2)
	case pageTagForm:
		body = m.settingsFormLines(w-2, "tag")
	}
	body = trimToHeight(body, innerH-2)
	// The bottom of the box is fixed: a separator, the description of the
	// current entry, and the keymap. Putting the description here rather than
	// under the row it belongs to is what stops the settings from shifting by
	// a row every time the cursor moves.
	footer := stDim.Render(hint)
	if s.status != "" {
		if s.statusK {
			footer = stErr.Render(s.status)
		} else {
			footer = stOK.Render(s.status)
		}
	}
	for len(body) < innerH-3 {
		body = append(body, "")
	}
	desc := m.settingsDescription(w - 2)
	content := strings.Join(body, "\n") +
		"\n" + stFaint.Render(strings.Repeat("─", maxInt(w-2, 0))) +
		"\n" + desc +
		"\n" + footer

	title := m.settingsBreadcrumb()
	return panel(title, content, w, h, true)
}

// settingsDescription is the one-line explanation of whatever the cursor is on,
// shown on its own fixed row at the bottom of the modal.
func (m *Model) settingsDescription(w int) string {
	s := m.settings
	if s == nil {
		return ""
	}
	text := ""
	switch s.page {
	case pageRoot:
		if s.cursor >= 0 && s.cursor < len(s.root) {
			text = s.root[s.cursor].help
		}
	case pageToolForm, pageTagForm:
		// The form's own description row explains the field being edited.
		if s.cursor >= 0 && s.cursor < len(s.form) {
			text = s.form[s.cursor].help
		}
	case pageTools:
		if s.cursor >= 0 && s.cursor < len(s.tools) {
			t := s.tools[s.cursor]
			text = t.Type + ": " + t.Command
		}
	case pageTags:
		if s.cursor >= 0 && s.cursor < len(s.tags) {
			t := s.tags[s.cursor]
			if t.Description == "" {
				text = "no description yet — press enter to add one"
			} else {
				text = t.Description
			}
		}
	}
	if text == "" {
		return ""
	}
	return stDim.Render(app.Truncate(text, maxInt(w, 8)))
}

// settingsBreadcrumb is the panel title of the current settings page.
func (m *Model) settingsBreadcrumb() string {
	switch m.settings.page {
	case pageTools, pageToolForm:
		return "settings › tools"
	case pageTags, pageTagForm:
		return "settings › tags"
	}
	return "settings"
}

func (m *Model) settingsRootLines(w int) []string {
	s := m.settings
	lines := []string{}
	for i, f := range s.fields() {
		cur := i == s.cursor
		mark := "  "
		if cur {
			mark = stAccent.Render("› ")
		}
		label := stFaint.Render(padRight(f.label, 18))
		var value string
		switch {
		case f.toggle:
			if f.on {
				value = stAccent.Render("[x] on")
			} else {
				value = stDim.Render("[ ] off")
			}
		case f.options != nil:
			value = stFaint.Render("◀ ") + f.input.Value() + stFaint.Render(" ▶")
		default:
			value = stDim.Render("…")
		}
		if cur {
			label = stFaint.Render(padRight(f.label, 18))
		}
		lines = append(lines, mark+label+value)
	}
	return lines
}

func (m *Model) settingsToolLines(w int) []string {
	s := m.settings
	var lines []string
	if len(s.tools) == 0 {
		return append(lines, stDim.Render("  no tools yet — press a to add one"))
	}
	lines = append(lines, "  "+stDim.Render(padRight("name", 14)+padRight("type", 6)+"command"))
	h := m.settingsListHeight()
	start := s.off
	end := minInt(start+h, len(s.tools))
	for i := start; i < end; i++ {
		t := s.tools[i]
		mark := "  "
		if i == s.cursor {
			mark = stAccent.Render("› ")
		}
		name := padRight(app.Truncate(t.Name, 13), 14)
		typ := padRight(t.Type, 6)
		isDefault := strings.EqualFold(t.Name, m.lib.Store.Config.DefaultTool)
		cmd := app.Truncate(t.Command, maxInt(w-26, 8))
		if i == s.cursor {
			// The cursor is one reverse bar; the default marker is plain text
			// inside it, because a colour would fight the reversed background.
			row := name + typ + cmd
			if isDefault {
				row += " *"
			}
			lines = append(lines, stCursor.Render(fit(" "+row, w)))
		} else {
			row := mark + stFaint.Render(name+typ+cmd)
			if isDefault {
				row += stTag.Render(" *")
			}
			lines = append(lines, row)
		}
	}
	return lines
}

func (m *Model) settingsTagLines(w int) []string {
	s := m.settings
	var lines []string
	if len(s.tags) == 0 {
		return append(lines, stDim.Render("  no tags yet — press a to add one"))
	}
	lines = append(lines, "  "+stDim.Render(padRight("tag", 18)+"used  description"))
	h := m.settingsListHeight()
	start := s.off
	end := minInt(start+h, len(s.tags))
	for i := start; i < end; i++ {
		t := s.tags[i]
		mark := "  "
		if i == s.cursor {
			mark = stAccent.Render("› ")
		}
		name := padRight(app.Truncate(t.Name, 17), 18)
		used := padRight(fmt.Sprintf("%d", s.tagUse[t.Name]), 6)
		desc := app.Truncate(t.Description, maxInt(w-28, 8))
		if i == s.cursor {
			lines = append(lines, stCursor.Render(fit(" "+name+used+desc, w)))
		} else {
			lines = append(lines, mark+stTag.Render(name)+stFaint.Render(used+desc))
		}
	}
	return lines
}

func (m *Model) settingsFormLines(w int, kind string) []string {
	s := m.settings
	lines := []string{}
	for i, f := range s.fields() {
		cur := i == s.cursor
		mark := "  "
		if cur {
			mark = stAccent.Render("› ")
		}
		lines = append(lines, mark+stFaint.Render(padRight(f.label, 14)))
		width := maxInt(w-18, 8)
		if f.options != nil {
			lines = append(lines, "    "+stFaint.Render("◀ ")+f.input.Value()+stFaint.Render(" ▶")+
				stDim.Render("  (space to change)"))
			continue
		}
		f.input.Width = width
		// Focus is owned by the key handling, not the view: focusing here left
		// the previous field's cursor on screen, because blurring it was
		// nobody's job.
		lines = append(lines, "    "+f.input.View())
	}
	return lines
}

// viewModal floats box over the browser. The browser behind is drawn and then
// dimmed, which is what makes the box read as a dialog rather than a separate
// screen: the content the user was looking at is still visible around it.
func (m *Model) viewModal(box string) string {
	if m.width < 20 || m.height < 6 {
		return box
	}
	base := dimScreen(m.viewBrowser())
	return overlayCard(box, base, m.width, m.height)
}

// dimScreen renders text in a single dim foreground. It is applied to the
// browser behind a modal so the modal is clearly the thing in focus while the
// layout behind stays recognisable.
func dimScreen(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if strings.TrimSpace(stripEsc(l)) == "" {
			continue
		}
		lines[i] = stDim.Render(stripEsc(l))
	}
	return strings.Join(lines, "\n")
}

// clampScreen pads or clips any view to exactly the terminal, so a page that
// bails out early with a short message ("terminal too small") still cannot
// scroll the screen or overflow the width.
func (m *Model) clampScreen(s string) string {
	if m.width <= 0 || m.height <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) > m.height {
		lines = lines[:m.height]
	}
	for len(lines) < m.height {
		lines = append(lines, "")
	}
	for i, l := range lines {
		lines[i] = fit(l, m.width)
	}
	return strings.Join(lines, "\n")
}

// viewConfirm draws a yes/no dialog over the dimmed browser.
func (m *Model) viewConfirm() string {
	c := m.confirm
	if c == nil {
		return m.clampScreen(m.viewBrowser())
	}
	if m.width < 20 || m.height < 6 {
		return m.clampScreen(m.viewBrowser())
	}
	boxW := 60
	if m.width-4 < boxW {
		boxW = maxInt(m.width-4, 16)
	}
	inner := boxW - 2
	var body []string
	for _, l := range c.lines {
		if l == "" {
			body = append(body, "")
			continue
		}
		body = append(body, wrapText(" "+l, maxInt(inner-1, 8))...)
	}
	body = append(body, "")
	// Two buttons; the focused one is reverse video, like every other cursor.
	yes, no := "[ "+c.accept+" ]", "[ cancel ]"
	var buttonRow string
	if c.yes {
		buttonRow = " " + stDim.Render(no) + "  " + stCursor.Render(yes)
	} else {
		buttonRow = " " + stCursor.Render(no) + "  " + stDim.Render(yes)
	}
	body = append(body, buttonRow)

	boxH := len(body) + 2
	if boxH > m.height {
		boxH = m.height
	}
	box := panel(c.title, strings.Join(body, "\n"), boxW, boxH, true)
	return overlayCard(box, dimScreen(m.viewBrowser()), m.width, m.height)
}
