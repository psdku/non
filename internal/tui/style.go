package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Visual language, after yazi's default look:
//
//   - The screen is split into bordered panes drawn with a single line box.
//   - The focused pane brightens its border; the others stay gray. That is the
//     only focus indicator, so the layout never shifts.
//   - The current line is marked with a reverse-video bar (yazi's `indicator`),
//     not a coloured arrow, so wide characters and long names behave.
//   - A status bar sits along the bottom, split into blocks; the mode label is
//     the first block, like yazi's `mode` section.
//   - Accents are used sparingly: cyan for the current working directory and
//     keys, magenta for hovered/selected labels, yellow for tags and find hits.
//
// No nerd font icons and no powerline separators: only box drawing characters
// and ordinary ASCII/Unicode text, so the UI works in any terminal font.

var (
	// colBorder is the inactive panel border.
	colBorder = lipgloss.Color("240")
	// colBorderActive is the focused panel border.
	colBorderActive = lipgloss.Color("33") // blue, as yazi's `[which] border`
	// colCwd is the current directory / app title accent.
	colCwd = lipgloss.Color("36") // cyan
	// colKey is the key hint colour.
	colKey = lipgloss.Color("36") // cyan
	// colAccent is used for the selected item's label.
	colAccent = lipgloss.Color("13") // magenta
	// colTag is used for tags and for search hits.
	colTag = lipgloss.Color("11") // yellow
	// colDim is secondary text.
	colDim = lipgloss.Color("245")
	// colFaint is tertiary text.
	colFaint = lipgloss.Color("242")
	// colErr is an error.
	colErr = lipgloss.Color("9") // red
	// colOK is a success message.
	colOK = lipgloss.Color("10") // green
)

var (
	stTitle  = lipgloss.NewStyle().Bold(true)
	stCwd    = lipgloss.NewStyle().Foreground(colCwd)
	stDim    = lipgloss.NewStyle().Foreground(colDim)
	stFaint  = lipgloss.NewStyle().Foreground(colFaint)
	stKey    = lipgloss.NewStyle().Foreground(colKey)
	stAccent = lipgloss.NewStyle().Foreground(colAccent)
	stTag    = lipgloss.NewStyle().Foreground(colTag)
	stErr    = lipgloss.NewStyle().Foreground(colErr)
	stOK     = lipgloss.NewStyle().Foreground(colOK)

	// stCursor is yazi's current-line indicator: reverse video over the whole
	// row, no arrow, no background colour of its own.
	stCursor = lipgloss.NewStyle().Reverse(true)

	// stSectionTitle is a panel title, drawn into the top border.
	stSectionTitle = lipgloss.NewStyle().Foreground(colCwd)

	// stBarMode is the leading mode block, reverse video like yazi's mode.
	stBarMode = lipgloss.NewStyle().Reverse(true).Bold(true)
)

// boxStyle is the border colour of a panel. Both states use the same single
// line glyphs; only the colour changes, so the layout never shifts on focus.
func boxStyle(focused bool) lipgloss.Style {
	if focused {
		return lipgloss.NewStyle().Foreground(colBorderActive)
	}
	return lipgloss.NewStyle().Foreground(colBorder)
}

// panel draws content inside a bordered box of exactly w×h cells, with an
// optional title written into the top border (yazi's panel titles).
//
// The box is drawn by hand rather than with a lipgloss border because the title
// has to sit inside the top rule, and a per-row build keeps the size exact for
// any content, including text wider than the box.
func panel(title string, content string, w, h int, focused bool) string {
	if w < 2 || h < 2 {
		return fitLines(strings.Split(content, "\n"), maxInt(h, 1), maxInt(w, 1))
	}
	bs := boxStyle(focused)
	innerW, innerH := w-2, h-2

	lines := strings.Split(content, "\n")
	lines = trimToHeight(lines, innerH)
	for len(lines) < innerH {
		lines = append(lines, "")
	}

	out := make([]string, 0, h)
	out = append(out, topRule(title, w, bs))
	for _, l := range lines {
		out = append(out, bs.Render("│")+fit(l, innerW)+bs.Render("│"))
	}
	out = append(out, bs.Render("└"+strings.Repeat("─", innerW)+"┘"))
	return strings.Join(out, "\n")
}

// topRule renders "╭ title ──────╮" to exactly w cells.
func topRule(title string, w int, bs lipgloss.Style) string {
	if title == "" {
		return bs.Render("┌" + strings.Repeat("─", w-2) + "┐")
	}
	label := " " + title + " "
	room := w - 2
	if lipgloss.Width(label) > room {
		label = ansi.Truncate(label, room, "")
	}
	dashes := room - lipgloss.Width(label)
	return bs.Render("┌") + stSectionTitle.Render(label) +
		bs.Render(strings.Repeat("─", dashes)+"┐")
}

// stripEsc removes escape sequences, leaving plain text. The test helpers use
// the spelling stripAnsi, so both are provided from one place.
func stripEsc(s string) string { return ansi.Strip(s) }

// stripAnsi removes escape sequences, leaving plain text.
func stripAnsi(s string) string { return stripEsc(s) }

// fitLines clips or pads lines to exactly h rows of at most w cells.
func fitLines(lines []string, h, w int) string {
	lines = trimToHeight(lines, h)
	for len(lines) < h {
		lines = append(lines, "")
	}
	for i, l := range lines {
		lines[i] = fit(l, w)
	}
	return strings.Join(lines, "\n")
}

// fitBlock forces a rendered block to exactly w×h.

// overlayCard draws box over base, clipping base so the box sits in the middle
// as a floating dialog. Unlike a full overlay, the rows above and below the box
// keep showing the browser, which is what makes it read as a popup.
func overlayCard(box, base string, w, h int) string {
	bl := strings.Split(base, "\n")
	for len(bl) < h {
		bl = append(bl, "")
	}
	bw, bh := lipgloss.Width(box), lipgloss.Height(box)
	top := (h - bh) / 2
	if top < 0 {
		top = 0
	}
	left := (w - bw) / 2
	if left < 0 {
		left = 0
	}
	out := make([]string, 0, h)
	for i := 0; i < h; i++ {
		line := ""
		if i < len(bl) {
			line = bl[i]
		}
		row := i - top
		if row < 0 || row >= bh {
			out = append(out, fit(line, w))
			continue
		}
		boxLine := strings.Split(box, "\n")[row]
		// Keep the left slice of the background, the box, then the rest.
		prefix := fit(ansi.Truncate(line, left, ""), left)
		rest := ansi.TruncateLeft(line, left+bw, "")
		out = append(out, prefix+boxLine+fit(rest, maxInt(w-left-bw, 0)))
	}
	return strings.Join(out, "\n")
}

// joinRows glues two same-height blocks together line by line.
func joinRows(a, b string) string {
	al := strings.Split(a, "\n")
	bl := strings.Split(b, "\n")
	n := maxInt(len(al), len(bl))
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		var l, r string
		if i < len(al) {
			l = al[i]
		}
		if i < len(bl) {
			r = bl[i]
		}
		out = append(out, l+r)
	}
	return strings.Join(out, "\n")
}
