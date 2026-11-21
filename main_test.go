package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"non/internal/app"
	"non/internal/meta"
)

func args(s ...string) []string { return s }

// reorderArgs lets flags follow the directory, which the flag package alone
// would silently ignore (launching the TUI instead of listing).
func TestReorderArgs(t *testing.T) {
	boolFlags := map[string]bool{
		"list": true, "count": true, "paths": true, "no-write": true,
		"search-meta": true, "version": true,
	}
	isBool := func(n string) bool { return boolFlags[n] }

	cases := []struct {
		in   []string
		want []string
	}{
		// Already correct: untouched.
		{args("--list", "."), args("--list", ".")},
		{args("--list"), args("--list")},
		{args(), args()},
		{args("."), args(".")},
		// Trailing flags move in front of the directory.
		{args(".", "--list"), args("--list", ".")},
		{args(".", "--list", "--count"), args("--list", "--count", ".")},
		// A value flag keeps its value next to it.
		{args(".", "--query", "paged"), args("--query", "paged", ".")},
		{args(".", "--tags", "LLM && !DB"), args("--tags", "LLM && !DB", ".")},
		{args(".", "--query=paged"), args("--query=paged", ".")},
		// Mixed.
		{args(".", "--query", "x", "--count"), args("--query", "x", "--count", ".")},
		// After --, nothing is a flag.
		{args("--", "."), args("--", ".")},
	}
	for _, c := range cases {
		got, err := reorderArgs(c.in, isBool)
		if err != nil {
			t.Errorf("reorderArgs(%q): %v", c.in, err)
			continue
		}
		if strings.Join(got, " ") != strings.Join(c.want, " ") {
			t.Errorf("reorderArgs(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestReorderArgsRejectsTwoDirectories(t *testing.T) {
	_, err := reorderArgs(args(".", ".."), func(string) bool { return false })
	if err == nil {
		t.Fatal("two directories should be an error, not a silent choice")
	}
	if !strings.Contains(err.Error(), "only one directory") {
		t.Errorf("unhelpful error: %v", err)
	}
}

// A flag that takes a value must keep it even when the name looks boolean-ish.
func TestReorderArgsMissingValue(t *testing.T) {
	got, err := reorderArgs(args(".", "--query"), func(string) bool { return false })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The value was absent, so the flag goes through alone and
	// flag.Parse reports the real problem.
	if strings.Join(got, " ") != "--query ." {
		t.Errorf("got %q", got)
	}
}

// --query prints bare file names, one per line: a script already knows what to
// do with them, and extra columns only have to be cut away again.
func TestPrintQueryOutputIsNamesOnly(t *testing.T) {
	dir := t.TempDir()
	write := func(name string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte("%PDF-1.4\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("alpha.pdf")
	write("beta.pdf")

	var buf bytes.Buffer
	stdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	lib, err := app.OpenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	lib.Store.Set("alpha.pdf", &meta.Entry{
		Title: "Alpha Paper", Author: "Someone", Tags: []string{"x"},
	})
	_ = lib.Rescan()
	printErr := printQuery(lib, app.Query{}, false, false, true)
	w.Close()
	os.Stdout = stdout
	if printErr != nil {
		t.Fatal(printErr)
	}
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want 2:\n%s", len(lines), buf.String())
	}
	for _, l := range lines {
		if strings.Contains(l, "\t") {
			t.Errorf("output has extra columns: %q", l)
		}
		if !strings.HasSuffix(l, ".pdf") {
			t.Errorf("not a file name: %q", l)
		}
	}
}

// --check reports problems and exits non-zero, without writing anything.
func TestCheckReportsUnknownTags(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.pdf"), []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw := `{"version":1,"entries":{"a.pdf":{"title":"A","tags":["LLM","typo"]}},
	         "config":{"tags":[{"name":"LLM"}]}}`
	path := filepath.Join(dir, meta.StoreFile)
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	lib, err := app.OpenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Capture stdout so the report can be checked.
	stdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	checkErr := runCheck(lib)
	w.Close()
	os.Stdout = stdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if checkErr == nil {
		t.Error("--check accepted a store with an unknown tag")
	}
	out := buf.String()
	if !strings.Contains(out, "typo") || !strings.Contains(out, "a.pdf") {
		t.Errorf("report does not name the problem:\n%s", out)
	}
	// Nothing was written.
	back, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(back) != raw {
		t.Errorf("--check modified the store:\n%s", back)
	}
}

func TestCheckCleanStoreSucceeds(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.pdf"), []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lib, err := app.OpenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := runCheck(lib); err != nil {
		t.Errorf("a clean store was reported as broken: %v", err)
	}
}

// An untidy-but-known name is reported by --check but does not fail: it is
// displayable and queryable, so a hook must not break on it.
func TestCheckToleratesUntidyName(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.pdf"), []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw := `{"version":1,"entries":{"a.pdf":{"title":"A","tags":["weird.name"]}},
	         "config":{"tags":[{"name":"weird.name"}]}}`
	if err := os.WriteFile(filepath.Join(dir, meta.StoreFile), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	lib, err := app.OpenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	checkErr := runCheck(lib)
	w.Close()
	os.Stdout = stdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if checkErr != nil {
		t.Errorf("--check failed on a tolerated name: %v", checkErr)
	}
	if !strings.Contains(buf.String(), "tolerated") {
		t.Errorf("the problem was not reported:\n%s", buf.String())
	}
}

// A name that would need quoting in every query does fail --check.
func TestCheckFailsOnCriticalName(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.pdf"), []byte("%PDF-1.4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw := `{"version":1,"entries":{"a.pdf":{"title":"A","tags":["KV cache"]}},
	         "config":{"tags":[{"name":"KV cache"}]}}`
	if err := os.WriteFile(filepath.Join(dir, meta.StoreFile), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	lib, err := app.OpenDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	stdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	checkErr := runCheck(lib)
	w.Close()
	os.Stdout = stdout
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)

	if checkErr == nil {
		t.Error("--check accepted a name that cannot be queried")
	}
	if !strings.Contains(buf.String(), "cannot be used in a query") {
		t.Errorf("report:\n%s", buf.String())
	}
}
