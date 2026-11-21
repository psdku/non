package app

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ledongthuc/pdf"

	"non/internal/extract"
)

// FileInfo is the system information shown by "Inspect".
type FileInfo struct {
	Name       string
	Dir        string
	Path       string
	Size       int64
	ModTime    string
	Mode       os.FileMode
	Pages      int
	Engine     string
	MetaTitle  string
	PDFTitle   string
	PDFAuthor  string
	PDFCreator string
	Producer   string
	Encrypted  string
	Warn       string
}

// Inspect collects system information about a PDF.
func Inspect(path string) FileInfo {
	fi := FileInfo{Name: filepath.Base(path), Dir: filepath.Dir(path), Path: path}
	st, err := os.Stat(path)
	if err != nil {
		fi.Warn = err.Error()
		return fi
	}
	fi.Size = st.Size()
	fi.ModTime = st.ModTime().Format("2006-01-02 15:04:05")
	fi.Mode = st.Mode()

	res := extract.Extract(path)
	fi.Pages = res.Pages
	fi.Engine = res.Engine
	fi.MetaTitle = res.MetaTitle
	fi.PDFTitle = res.Title
	fi.Warn = res.Warn

	// The Info dictionary has a few more fields worth showing.
	func() {
		defer func() { recover() }()
		f, r, err := pdf.Open(path)
		if err != nil {
			return
		}
		defer f.Close()
		if v := r.Trailer().Key("Encrypt"); !v.IsNull() {
			fi.Encrypted = "yes"
		} else {
			fi.Encrypted = "no"
		}
		info := r.Trailer().Key("Info")
		if info.IsNull() {
			return
		}
		fi.PDFAuthor = strings.TrimSpace(info.Key("Author").Text())
		fi.PDFCreator = strings.TrimSpace(info.Key("Creator").Text())
		fi.Producer = strings.TrimSpace(info.Key("Producer").Text())
	}()
	return fi
}

// String renders the inspection report as plain text lines.
func (fi FileInfo) String() string {
	var b strings.Builder
	row := func(k, v string) {
		if v == "" {
			v = "-"
		}
		fmt.Fprintf(&b, "%-11s %s\n", k+":", v)
	}
	row("File", fi.Name)
	row("Directory", fi.Dir)
	row("Size", humanSize(fi.Size))
	row("Modified", fi.ModTime)
	row("Mode", fi.Mode.String())
	row("Pages", itoaOr(fi.Pages))
	row("Encrypted", fi.Encrypted)
	row("Extractor", fi.Engine)
	if fi.PDFTitle != "" {
		row("Title", fi.PDFTitle)
	}
	row("PDF title", fi.MetaTitle)
	row("Author", fi.PDFAuthor)
	row("Creator", fi.PDFCreator)
	row("Producer", fi.Producer)
	if fi.Warn != "" {
		row("Note", fi.Warn)
	}
	return strings.TrimRight(b.String(), "\n")
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func itoaOr(n int) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("%d", n)
}

// FilenameChange describes what sanitizing a name did, so the caller can warn
// the user before the rename rather than silently rewriting their input.
type FilenameChange struct {
	Original string // what the user typed
	Clean    string // the name that will be used
	// Removed lists the characters that were dropped, described in words
	// ("quotes", "a newline"), de-duplicated and in a stable order.
	Removed []string
}

// Changed reports whether sanitizing altered the name.
func (c FilenameChange) Changed() bool { return c.Original != c.Clean }

// Warning is one line describing the change, or "" when nothing changed.
func (c FilenameChange) Warning() string {
	if !c.Changed() {
		return ""
	}
	if len(c.Removed) == 0 {
		return "name normalized to " + c.Clean
	}
	return "removed " + strings.Join(c.Removed, ", ") + " — using " + c.Clean
}

// CleanFilename is SanitizeFilename with a report of what it changed.
//
// The rules come from what actually breaks things downstream: a name is shown
// in a one-line list, typed into filter expressions, and stored as a JSON key.
//
//   - quotes are removed: they would need escaping everywhere the name is shown
//     or matched;
//   - a newline or tab becomes a single space: a name is displayed on one row;
//   - control characters are removed: they can move the cursor or corrupt the
//     rendered list;
//   - path separators are dropped rather than replaced, so a rename can never
//     escape the browsed directory.
//
// Runs of whitespace collapse to one dash, which keeps "New Name" readable and
// avoids names with stray double spaces.
func CleanFilename(name string) FilenameChange {
	orig := name
	// Reduce any path to its last component first: a rename is confined to the
	// browsed directory, so separators never reach the result.
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	var b strings.Builder
	var removed []string
	note := func(s string) {
		if slices.Contains(removed, s) {
			return
		}
		removed = append(removed, s)
	}
	for _, r := range name {
		switch {
		case r == '"' || r == '\'':
			note("quotes")
		case r == '\n' || r == '\r':
			note("a newline")
			b.WriteByte(' ')
		case r == '\t':
			note("a tab")
			b.WriteByte(' ')
		case r == 0 || unicode.IsControl(r):
			note("control characters")
		default:
			b.WriteRune(r)
		}
	}
	base := strings.Join(strings.Fields(b.String()), "-")
	if base == "" || base == "." || base == ".." {
		base = "untitled"
	}
	// A name that starts with a dash reads as a flag; a trailing one is noise.
	base = strings.Trim(base, "-")
	if base == "" {
		base = "untitled"
	}
	if !strings.HasSuffix(strings.ToLower(base), ".pdf") {
		base += ".pdf"
	}
	return FilenameChange{Original: orig, Clean: base, Removed: removed}
}

// SanitizeFilename turns arbitrary user input into a plain file name inside the
// browsed directory. See CleanFilename for the rules.
func SanitizeFilename(name string) string { return CleanFilename(name).Clean }

// SafeRename moves oldName to newName in dir, refusing to clobber a different
// existing file. It returns the final name.
func SafeRename(dir, oldName, newName string) (string, error) {
	newName = SanitizeFilename(newName)
	if newName == oldName {
		return oldName, nil
	}
	target := filepath.Join(dir, newName)
	if _, err := os.Stat(target); err == nil {
		return "", fmt.Errorf("%s already exists", newName)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	src := filepath.Join(dir, oldName)
	if err := os.Rename(src, target); err != nil {
		return "", err
	}
	return newName, nil
}

// Truncate shortens s to at most n runes, adding an ellipsis when cut.
func Truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if n <= 1 || utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n-1]) + "…"
}
