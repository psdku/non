// Package extract pulls a title and an abstract out of a PDF file.
//
// Two engines are used:
//
//   - poppler's pdftotext, when installed. `-bbox-layout` gives word boxes with
//     clean Unicode text, and line heights stand in for font sizes, which makes
//     finding the title reliable. PDF-level metadata titles are frequently
//     empty or junk, so layout matters more than the Info dictionary.
//   - a pure Go reader (github.com/ledongthuc/pdf) as a fallback, using glyph
//     positions and font sizes directly.
package extract

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
)

// Result is what extraction produced for one file.
type Result struct {
	Title    string // best guess at the paper title
	Abstract string // best guess at the abstract
	Pages    int    // page count, 0 when unknown
	Engine   string // "pdftotext+layout", "layout", "none"
	// MetaTitle is the title stored in the PDF's document information
	// dictionary, if any.
	MetaTitle string
	// Author is the author recorded in the PDF's Info dictionary.
	Author string
	// Warn holds non fatal problems (e.g. unreadable page one).
	Warn string
}

// TitleOr returns the title, falling back to a humanized file name.
func (r Result) TitleOr(fallback string) string {
	if t := strings.TrimSpace(r.Title); t != "" {
		return t
	}
	return TitleFromFilename(fallback)
}

var (
	popplerOnce sync.Once
	popplerOK   bool
)

// HasPDFToText reports whether the poppler `pdftotext` binary is available.
func HasPDFToText() bool {
	popplerOnce.Do(func() { popplerOK = lookPDFToText() != "" })
	return popplerOK
}

// Extract reads path and returns the best metadata it can find.
func Extract(path string) Result {
	var res Result

	if tool := lookPDFToText(); tool != "" {
		if doc, err := readPoppler(tool, path); err == nil {
			res.Pages = doc.pages
			res.MetaTitle = cleanMetaTitle(doc.metaTitle)
			res.Author = collapseSpaces(doc.metaAuthor)
			res.Engine = "pdftotext+layout"
			res.Title = doc.title()
			if t := res.MetaTitle; t != "" {
				if res.Title == "" {
					res.Title = t
				} else if similarTitle(res.Title, t) {
					res.Title = t
				}
			}
			if res.Author == "" {
				res.Author = doc.authors()
			}
			res.Abstract = doc.abstract()
			if res.Abstract == "" {
				res.Abstract = abstractFromLines(lines(doc.rawText))
			}
			if res.Warn == "" && doc.warn != "" {
				res.Warn = doc.warn
			}
			if res.Title != "" {
				return res
			}
		} else if res.Warn == "" {
			res.Warn = err.Error()
		}
	}

	// Pure Go fallback (also covers a poppler run that yielded no title).
	doc, err := readLayout(path)
	if err != nil {
		if res.Warn == "" {
			res.Warn = err.Error()
		}
		if res.Engine == "" {
			res.Engine = "none"
		}
		return res
	}
	if res.Pages == 0 {
		res.Pages = doc.pages
	}
	if res.MetaTitle == "" {
		res.MetaTitle = cleanMetaTitle(doc.metaTitle)
	}
	if res.Author == "" {
		res.Author = collapseSpaces(doc.metaAuthor)
	}
	if res.Title == "" {
		res.Title = doc.title()
	}
	if res.MetaTitle != "" {
		// The Info dictionary title is often the cleanest copy: it is not
		// affected by ligature or glyph problems in the fallback reader.
		switch {
		case res.Title == "":
			res.Title = res.MetaTitle
		case similarTitle(res.Title, res.MetaTitle):
			res.Title = res.MetaTitle
		}
	}
	if res.Abstract == "" {
		res.Abstract = doc.abstract()
	}
	if res.Author == "" {
		res.Author = doc.authors()
	}
	if res.Engine != "pdftotext+layout" {
		res.Engine = "layout"
	}
	return res
}

// TitleFromFilename turns a file name into a human readable title.
func TitleFromFilename(name string) string {
	base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	base = strings.NewReplacer("_", " ", "-", " ").Replace(base)
	base = collapseSpaces(base)
	if base == "" {
		return name
	}
	return base
}

func errf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}
