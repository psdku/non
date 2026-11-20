package extract

import (
	"bytes"
	"encoding/xml"
	"io"
	"os/exec"
	"sort"
	"strings"
)

// lookPDFToText resolves the poppler binary ("" when absent).
func lookPDFToText() string {
	p, err := exec.LookPath("pdftotext")
	if err != nil {
		// Some systems also ship it as pdftotext.exe or under a prefix.
		return ""
	}
	return p
}

// --- bbox-layout XML -------------------------------------------------------

type xmlPage struct {
	Width  float64   `xml:"width,attr"`
	Height float64   `xml:"height,attr"`
	Blocks []xmlFlow `xml:"flow>block"`
}

type xmlFlow struct {
	XMin  float64   `xml:"xMin,attr"`
	YMin  float64   `xml:"yMin,attr"`
	XMax  float64   `xml:"xMax,attr"`
	YMax  float64   `xml:"yMax,attr"`
	Lines []xmlLine `xml:"line"`
}

type xmlLine struct {
	XMin  float64   `xml:"xMin,attr"`
	YMin  float64   `xml:"yMin,attr"`
	XMax  float64   `xml:"xMax,attr"`
	YMax  float64   `xml:"yMax,attr"`
	Words []xmlWord `xml:"word"`
}

type xmlWord struct {
	Text string  `xml:",chardata"`
	XMin float64 `xml:"xMin,attr"`
	XMax float64 `xml:"xMax,attr"`
	YMin float64 `xml:"yMin,attr"`
	YMax float64 `xml:"yMax,attr"`
}

type xmlDoc struct {
	Title string    `xml:"head>title"`
	Pages []xmlPage `xml:"body>doc>page"`
}

// popplerDoc holds what poppler told us about a file.
type popplerDoc struct {
	pages      int
	metaTitle  string
	metaAuthor string
	warn       string
	pageW      float64
	lines      []line // page one, reading order
	rawText    string
}

func readPoppler(tool, path string) (*popplerDoc, error) {
	doc := &popplerDoc{}

	out, err := runPDFToText(tool, "-bbox-layout", "-f", "1", "-l", "1", path)
	if err != nil {
		return nil, err
	}
	var parsed xmlDoc
	dec := xml.NewDecoder(bytes.NewReader(out))
	dec.Strict = false
	if err := dec.Decode(&parsed); err != nil && err != io.EOF {
		return nil, errf("parse bbox layout: %v", err)
	}
	if len(parsed.Pages) == 0 {
		return nil, errf("no page data")
	}
	pg := parsed.Pages[0]
	doc.pageW = pg.Width
	doc.metaTitle = strings.TrimSpace(parsed.Title)

	for _, blk := range pg.Blocks {
		for _, ln := range blk.Lines {
			l := line{x: ln.XMin, y: -ln.YMin, size: ln.YMax - ln.YMin}
			var words []string
			var seg strings.Builder
			segStart := 0.0
			flush := func() {
				if t := strings.TrimSpace(seg.String()); t != "" {
					l.segments = append(l.segments, segment{x: segStart, text: t})
				}
				seg.Reset()
			}
			for _, w := range ln.Words {
				t := collapseSpaces(w.Text)
				if t == "" {
					continue
				}
				words = append(words, t)
				if seg.Len() == 0 {
					segStart = w.XMin
					seg.WriteString(t)
				} else {
					seg.WriteByte(' ')
					seg.WriteString(t)
				}
			}
			flush()
			l.text = strings.Join(words, " ")
			if l.text == "" {
				continue
			}
			// Vertical text (axis labels, arxiv sidebars) must never become
			// the title. Its word boxes are taller than they are wide.
			if isRotatedWords(ln.Words) {
				l.rotated = true
			}
			doc.lines = append(doc.lines, l)
		}
	}
	sort.SliceStable(doc.lines, func(i, j int) bool {
		if absf(doc.lines[i].y-doc.lines[j].y) > 1 {
			return doc.lines[i].y > doc.lines[j].y
		}
		return doc.lines[i].x < doc.lines[j].x
	})

	doc.pages, doc.metaTitle, doc.metaAuthor = docInfo(path)
	if doc.metaTitle == "" {
		doc.metaTitle = strings.TrimSpace(parsed.Title)
	}

	if raw, err := runPDFToText(tool, "-raw", "-f", "1", "-l", "2", path); err == nil {
		doc.rawText = string(raw)
	} else if doc.warn == "" {
		doc.warn = err.Error()
	}
	return doc, nil
}

func runPDFToText(tool string, args ...string) ([]byte, error) {
	full := append([]string{"-enc", "UTF-8"}, args...)
	full = append(full, "-")
	cmd := exec.Command(tool, full...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, errf("pdftotext: %s", msg)
	}
	return out.Bytes(), nil
}

// isRotatedWords reports whether the words of a line are set vertically, which
// poppler reports as boxes that are much taller than they are wide.
func isRotatedWords(ws []xmlWord) bool {
	tall := 0
	for _, w := range ws {
		h := w.YMax - w.YMin
		wide := w.XMax - w.XMin
		if h > 2*wide && h > 6 {
			tall++
		}
	}
	return tall > 0 && tall*2 >= len(ws)
}

// --- title / abstract from the layout -------------------------------------

// title picks the title from page-one lines.
func (d *popplerDoc) title() string { return titleFromLines(d.lines) }

// authors returns the author block found by the shared heuristic.
func (d *popplerDoc) authors() string { return authorsFromLines(d.lines) }

// abstract finds the abstract heading on page one and reads the text that
// follows it inside the same column.
func (d *popplerDoc) abstract() string {
	lines := d.lines
	start := -1
	for i, l := range lines {
		if isAbstractHeading(l.text) {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	colX := lines[start].x
	pageW := d.pageW
	if pageW <= 0 {
		pageW = 612
	}
	var out []string
	blank := 0
	for _, l := range lines[start+1:] {
		if l.rotated {
			continue // vertical text: watermarks, axis labels
		}
		if l.x > colX+0.22*pageW {
			continue // other column, caption, figure label
		}
		if isStopLine(l.text) || (l.size > 0 && l.size > 1.6*lines[start].size) {
			break
		}
		var keep []string
		for _, s := range l.segments {
			if s.x <= colX+0.22*pageW {
				keep = append(keep, s.text)
			}
		}
		if len(keep) == 0 {
			if len(out) > 0 {
				if blank++; blank > 2 {
					break
				}
			}
			continue
		}
		blank = 0
		out = append(out, joinSegments(keep))
		if len(out) > 45 {
			break
		}
	}
	return cleanAbstract(joinLines(out))
}

func joinSegments(segs []string) string {
	var b strings.Builder
	for i, s := range segs {
		switch {
		case i == 0:
			b.WriteString(s)
		case strings.HasSuffix(b.String(), "-"):
			b.WriteString(s)
		default:
			b.WriteByte(' ')
			b.WriteString(s)
		}
	}
	return b.String()
}

// docInfo reads the page count plus the Info dictionary title and author.
// pdfinfo is used when available because it is much faster than parsing the
// whole file, and because it decodes the Info strings for us.
func docInfo(path string) (pages int, title, author string) {
	if p, err := exec.LookPath("pdfinfo"); err == nil {
		cmd := exec.Command(p, path)
		var out bytes.Buffer
		cmd.Stdout = &out
		if err := cmd.Run(); err == nil {
			for _, l := range lines(out.String()) {
				k, v, ok := strings.Cut(l, ":")
				if !ok {
					continue
				}
				v = strings.TrimSpace(v)
				switch strings.TrimSpace(k) {
				case "Pages":
					pages = atoi(v)
				case "Title":
					title = v
				case "Author":
					author = v
				}
			}
			if pages > 0 {
				return pages, title, author
			}
		}
	}
	return pagesFromPDF(path), "", ""
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return n
		}
		n = n*10 + int(r-'0')
	}
	return n
}
