package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"non/internal/meta"
)

// makeDir creates a temp dir with the given files (empty PDF stubs).
func makeDir(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("%PDF-1.4\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestOpenDirFindsPDFs(t *testing.T) {
	dir := makeDir(t, "a.pdf", "B.PDF", "notes.txt", "c.pdf")
	if err := os.Mkdir(filepath.Join(dir, "sub.pdf"), 0o755); err != nil {
		t.Fatal(err)
	}
	lib, err := OpenDir(dir)
	if err != nil {
		t.Fatalf("OpenDir: %v", err)
	}
	var got []string
	for _, p := range lib.Papers {
		got = append(got, p.Name)
	}
	// Sorted case-insensitively: a.pdf, B.PDF, c.pdf.
	want := []string{"a.pdf", "B.PDF", "c.pdf"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("papers = %v, want %v", got, want)
	}
}

func TestOpenDirEmpty(t *testing.T) {
	lib, err := OpenDir(t.TempDir())
	if err != nil {
		t.Fatalf("OpenDir: %v", err)
	}
	if len(lib.Papers) != 0 {
		t.Errorf("papers = %v", lib.Papers)
	}
}

func TestPaperDisplay(t *testing.T) {
	p := &Paper{Name: "my_paper.pdf"}
	if got := p.Display(); got != "my paper" {
		t.Errorf("Display = %q", got)
	}
	p.Meta = &meta.Entry{Title: "Real Title"}
	if got := p.Display(); got != "Real Title" {
		t.Errorf("Display = %q", got)
	}
	if !p.HasMeta() {
		t.Error("HasMeta = false for a titled entry")
	}
	if (&Paper{Name: "x.pdf", Meta: &meta.Entry{}}).HasMeta() {
		t.Error("HasMeta = true for an empty entry")
	}
}

func TestEnsureMetaPersists(t *testing.T) {
	dir := makeDir(t, "a.pdf")
	lib, err := OpenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := lib.Paper("a.pdf")
	e, err := lib.EnsureMeta(p, false)
	if err != nil {
		t.Fatalf("EnsureMeta: %v", err)
	}
	// A stub file has no text, so the title falls back to the file name.
	if e.Title != "a" {
		t.Errorf("Title = %q, want %q", e.Title, "a")
	}
	if !e.Auto {
		t.Error("a freshly extracted entry should be marked automatic")
	}
	if _, err := os.Stat(filepath.Join(dir, meta.StoreFile)); err != nil {
		t.Errorf("store not written: %v", err)
	}
	// A second call must not re-extract or change anything.
	e2, err := lib.EnsureMeta(p, false)
	if err != nil {
		t.Fatal(err)
	}
	if e2.Title != e.Title {
		t.Errorf("title changed on second call: %q -> %q", e.Title, e2.Title)
	}
}

func TestEnsureMetaKeepsManualEdits(t *testing.T) {
	dir := makeDir(t, "a.pdf")
	lib, err := OpenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	manual := &meta.Entry{Title: "Hand Written", Author: "Me", Tags: []string{"mine"}}
	if err := lib.SaveMeta("a.pdf", manual); err != nil {
		t.Fatal(err)
	}
	p := lib.Paper("a.pdf")
	e, err := lib.EnsureMeta(p, false)
	if err != nil {
		t.Fatal(err)
	}
	if e.Title != "Hand Written" || e.Author != "Me" || len(e.Tags) != 1 {
		t.Errorf("manual metadata was overwritten: %+v", e)
	}
	if e.Auto {
		t.Error("manual entry marked automatic")
	}
}

func TestRenamePaperMovesMetadata(t *testing.T) {
	dir := makeDir(t, "old.pdf")
	lib, err := OpenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := lib.SaveMeta("old.pdf", &meta.Entry{Title: "T"}); err != nil {
		t.Fatal(err)
	}
	final, err := lib.RenamePaper("old.pdf", "new.pdf")
	if err != nil {
		t.Fatalf("RenamePaper: %v", err)
	}
	if final != "new.pdf" {
		t.Errorf("final = %q", final)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.pdf")); err != nil {
		t.Errorf("file not renamed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "old.pdf")); !os.IsNotExist(err) {
		t.Error("old file still present")
	}
	if _, ok := lib.Store.Lookup("new.pdf"); !ok {
		t.Error("metadata did not follow the rename")
	}
	if lib.Paper("new.pdf") == nil {
		t.Error("library not rescanned after rename")
	}
}

func TestRenamePaperRefusesClobber(t *testing.T) {
	dir := makeDir(t, "a.pdf", "b.pdf")
	lib, err := OpenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lib.RenamePaper("a.pdf", "b.pdf"); err == nil {
		t.Fatal("renaming onto an existing file should fail")
	}
	if _, err := os.Stat(filepath.Join(dir, "a.pdf")); err != nil {
		t.Error("source file lost after a refused rename")
	}
}

func TestSanitizeFilename(t *testing.T) {
	cases := map[string]string{
		"new.pdf":  "new.pdf",
		"New Name": "New-Name.pdf",
		// Path components are stripped: a rename must not escape the directory
		// being browsed.
		"weird/name.pdf": "name.pdf",
		"../escape.pdf":  "escape.pdf",
		"a  b.pdf":       "a-b.pdf",
		"sub/dir/x.pdf":  "x.pdf",
		"no ext":         "no-ext.pdf",
		"":               "untitled.pdf",
	}
	for in, want := range cases {
		if got := SanitizeFilename(in); got != want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := Truncate("short", 10); got != "short" {
		t.Errorf("Truncate = %q", got)
	}
	if got := Truncate("a much longer string", 6); got != "a muc…" {
		t.Errorf("Truncate = %q", got)
	}
	if got := Truncate("line\nbreak", 20); got != "line break" {
		t.Errorf("Truncate = %q", got)
	}
}

func TestHumanSize(t *testing.T) {
	cases := map[int64]string{
		512:             "512 B",
		2048:            "2.0 KB",
		5 * 1024 * 1024: "5.0 MB",
	}
	for in, want := range cases {
		if got := humanSize(in); got != want {
			t.Errorf("humanSize(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestInspectMissingFile(t *testing.T) {
	fi := Inspect(filepath.Join(t.TempDir(), "nope.pdf"))
	if fi.Warn == "" {
		t.Error("expected a warning for a missing file")
	}
	if !strings.Contains(fi.String(), "nope.pdf") {
		t.Errorf("report does not mention the file:\n%s", fi.String())
	}
}

func TestInspectRealPDF(t *testing.T) {
	dir, err := filepath.Abs("../../samples")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "1706.03762v7.pdf")
	if _, err := os.Stat(path); err != nil {
		t.Skip("sample PDF not present")
	}
	fi := Inspect(path)
	if fi.Size == 0 {
		t.Error("Size = 0")
	}
	if fi.Pages != 15 {
		t.Errorf("Pages = %d", fi.Pages)
	}
	rep := fi.String()
	for _, want := range []string{"Pages:", "Size:", "Modified:", "Mode:"} {
		if !strings.Contains(rep, want) {
			t.Errorf("report is missing %q:\n%s", want, rep)
		}
	}
}
