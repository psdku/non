// Command non is a small manager for PDF papers in a directory.
//
//	non                 browse the *.pdf files in the current directory
//	non DIR             browse another directory
//	non --opener CMD    add CMD as the default tool (e.g. firefox)
//	non --tool SPEC     add a tool, SPEC is "name=command[,tui|gui]"
//	non --editor CMD    edit metadata with CMD
//	non --extract FILE  print the extracted metadata for FILE and exit
//	non --version
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"non/internal/app"
	"non/internal/extract"
	"non/internal/filter"
	"non/internal/meta"
	"non/internal/tui"
)

var version = "0.6.2"

// stringList collects a repeatable string flag.
type stringList []string

func (l *stringList) String() string { return strings.Join(*l, ",") }

func (l *stringList) Set(v string) error {
	*l = append(*l, v)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "non:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		opener   = flag.String("opener", "", "add this program as the default tool, e.g. firefox")
		tools    stringList
		editor   = flag.String("editor", "", "editor used for metadata, e.g. nvim")
		showVer  = flag.Bool("version", false, "print the version and exit")
		extractF = flag.String("extract", "", "print extracted metadata for the given PDF and exit")
		list     = flag.Bool("list", false, "print the collected papers and their metadata, then exit")
		noWrite  = flag.Bool("no-write", false, "with --extract, do not touch "+meta.StoreFile)
		importF  = flag.String("import", "", "import tags from a CSV file (name,description)")
		exportF  = flag.String("export", "", "write the tags as CSV to this file, or - for stdout")
		global   = flag.Bool("g", false, "with --import/--export, use the user config instead of this collection")
		setDir   = flag.String("set-dir", "", "set the default directory in the user config")
		unsetDir = flag.Bool("unset-dir", false, "clear the default directory")
		showDef  = flag.Bool("default-dir", false, "print the default directory and exit")
		queryF   = flag.String("query", "", "print the papers matching this text query, then exit")
		tagsF    = flag.String("tags", "", "with --query/--list, keep only papers matching this tag expression")
		paths    = flag.Bool("paths", false, "with --query, print full paths instead of a table")
		count    = flag.Bool("count", false, "with --query, print only the number of matches")
		metaInQ  = flag.Bool("search-meta", false, "with --query, also match author, tags and abstract")
	)
	flag.Var(&tools, "tool", "add a tool: name=command[,tui|gui] (repeatable)")
	flag.Usage = usage
	// The flag package stops at the first positional argument, so `non . --list`
	// would quietly ignore --list and open the TUI. Reorder instead, but only
	// when the trailing tokens are unambiguously flags, so a directory whose
	// name begins with a dash still works.
	args, err := reorderArgs(os.Args[1:], isBoolFlag)
	if err != nil {
		return err
	}
	if err := flag.CommandLine.Parse(args); err != nil {
		return err
	}

	if *showVer {
		fmt.Println("non", version)
		return nil
	}

	// The user config is read before a directory is chosen, because it may
	// hold the default directory.
	gcfg, err := meta.LoadGlobal()
	if err != nil {
		return err
	}
	// Directory flags act on the user config alone, so they work from anywhere.
	if *setDir != "" || *unsetDir || *showDef {
		return runDirFlags(gcfg, *setDir, *unsetDir, *showDef)
	}
	// Tag import and export.
	if *importF != "" || *exportF != "" {
		return runTagFlags(gcfg, *importF, *exportF, *global)
	}

	dir, err := resolveDir(flag.Args(), gcfg)
	if err != nil {
		return err
	}

	if *extractF != "" {
		return printExtract(*extractF)
	}

	lib, err := app.OpenDirWith(dir, gcfg)
	if err != nil {
		return err
	}
	// Tool flags are applied before anything reads the store, so --list and
	// the TUI always see the same configuration.
	if err := applyToolFlags(lib, *opener, tools); err != nil {
		return err
	}
	// "Was the flag given?" is not the same as "is its value non-empty": an
	// empty --query is a legitimate way to ask for the tagged papers in
	// library order, so the flags are detected by presence.
	set := map[string]bool{}
	flag.Visit(func(f *flag.Flag) { set[f.Name] = true })

	// A tag expression is valid on its own (--list --tags LLM), and together
	// with --query it narrows the candidates before ranking.
	var tagNode filter.Node
	if *tagsF != "" {
		node, err := filter.Parse(*tagsF)
		if err != nil {
			return fmt.Errorf("--tags: %w", err)
		}
		tagNode = node
		if unknown := filter.UnknownTags(node, knownTags(lib)); len(unknown) > 0 {
			fmt.Fprintf(os.Stderr, "non: warning: no such tag: %s\n", strings.Join(unknown, ", "))
		}
	}
	q := app.Query{
		Tags:     tagNode,
		Text:     *queryF,
		WithMeta: *metaInQ || lib.Store.Config.SearchMeta,
	}
	// --list and --query print the same records; --list adds the abstract.
	// Both honour --tags, so `--list --tags LLM` lists exactly the LLM papers.
	if *list {
		return printLibrary(lib, q, *noWrite)
	}
	if set["query"] || *tagsF != "" {
		return printQuery(lib, q, *paths, *count, *noWrite)
	}

	m, err := tui.Open(lib.Dir)
	if err != nil {
		return err
	}

	if *editor != "" {
		m.SetEditor(*editor)
	}
	opts := []tea.ProgramOption{tea.WithAltScreen()}
	if _, err := tea.NewProgram(m, opts...).Run(); err != nil {
		if errors.Is(err, tea.ErrProgramKilled) {
			return nil
		}
		return err
	}
	return nil
}

func usage() {
	out := flag.CommandLine.Output()
	fmt.Fprintf(out, `non %s — a simple manager for PDF papers

usage:
  non [flags] [DIR]

Directory priority: DIR argument > default directory > current directory.
Metadata is stored in %s inside the browsed directory; the default directory
lives in ~/.config/non/%s.

keys (inside the TUI):
  j/k ↑/↓ ctrl+n/p   move
  f                  filter by tag: LLM && !DB      /  fuzzy search
  tab                search metadata too
  enter              action menu   o      open with the default tool
  O                  pick a tool   r      rename
  m / ctrl+e         edit metadata ($EDITOR or the built-in form)
  e                  re-extract from the PDF     i  inspect
  s                  settings: tools, tags, editing mode
  J/K                scroll info   R      rescan    ?  help    q  quit

flags:
`, version, meta.StoreFile, meta.GlobalConfigFile)
	flag.PrintDefaults()
}

// applyToolFlags folds --opener and --tool into the stored configuration.
func applyToolFlags(lib *app.Library, opener string, tools []string) error {
	changed := false
	for _, spec := range tools {
		tool, err := meta.ParseToolSpec(spec)
		if err != nil {
			return err
		}
		if err := lib.Store.Config.AddTool(tool); err != nil {
			return fmt.Errorf("%s: %w", spec, err)
		}
		changed = true
	}
	// --opener is applied last so it becomes the default, which is what a user
	// passing it alone expects.
	if strings.TrimSpace(opener) != "" {
		lib.AddOpener(opener)
		changed = true
	}
	if !changed {
		return nil
	}
	return lib.Store.Save()
}

func printExtract(path string) error {
	res := extract.Extract(path)
	e := &meta.Entry{
		Title:    res.TitleOr(path),
		Author:   res.Author,
		Abstract: res.Abstract,
	}
	if strings.TrimSpace(e.Abstract) == "" && res.Warn != "" {
		fmt.Fprintln(os.Stderr, "note:", res.Warn)
	}
	fmt.Print(meta.FormatMarkdown(e))
	return nil
}

// printLibrary writes one line per paper plus its metadata, for scripting.
func printLibrary(lib *app.Library, q app.Query, noWrite bool) error {
	for _, i := range q.Select(lib) {
		p := lib.Papers[i]
		e, err := entryFor(lib, p, noWrite)
		if err != nil {
			return err
		}
		fmt.Printf("%s\t%s", p.Name, e.Title)
		if e.Author != "" {
			fmt.Printf("\t%s", e.Author)
		}
		if len(e.Tags) > 0 {
			fmt.Printf("\t%s", strings.Join(e.Tags, ","))
		}
		if e.Abstract != "" {
			a := strings.ReplaceAll(e.Abstract, "\n", " ")
			if r := []rune(a); len(r) > 100 {
				a = string(r[:100]) + "…"
			}
			fmt.Printf("\t%s", a)
		}
		fmt.Println()
	}
	return nil
}

// resolveDir picks the directory to browse.
//
// Priority, highest first: a command line argument, then the default directory
// from the user config, then the current directory. The argument may be a
// directory or a file, in which case its directory is browsed, so shell
// completion (which produces files) works.
func resolveDir(args []string, g *meta.GlobalConfig) (string, error) {
	if len(args) > 0 {
		arg := expandHome(args[0])
		fi, err := os.Stat(arg)
		switch {
		case err == nil && fi.IsDir():
			return arg, nil
		case err == nil:
			return filepath.Dir(arg), nil
		case os.IsNotExist(err):
			return "", fmt.Errorf("%s: no such file or directory", arg)
		default:
			return "", err
		}
	}
	if g != nil && g.DefaultDir != "" {
		dir := expandHome(g.DefaultDir)
		fi, err := os.Stat(dir)
		switch {
		case err == nil && fi.IsDir():
			return dir, nil
		case os.IsNotExist(err):
			return "", fmt.Errorf("default directory %s no longer exists (fix it with --set-dir)", dir)
		default:
			return "", err
		}
	}
	return ".", nil
}

// expandHome expands a leading ~ or ~/ using the current user's home.
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// runDirFlags handles the flags that only touch the user config.
func runDirFlags(g *meta.GlobalConfig, set string, unset, show bool) error {
	switch {
	case show:
		if g.DefaultDir == "" {
			fmt.Println("(unset: the current directory is used)")
			return nil
		}
		fmt.Println(g.DefaultDir)
		return nil
	case unset:
		g.DefaultDir = ""
		if err := g.Save(); err != nil {
			return err
		}
		fmt.Println("default directory cleared")
		return nil
	}
	dir := expandHome(strings.TrimSpace(set))
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%s: no such directory", abs)
		}
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%s: not a directory", abs)
	}
	g.DefaultDir = abs
	if err := g.Save(); err != nil {
		return err
	}
	fmt.Printf("default directory set to %s\n", abs)
	return nil
}

// runTagFlags imports or exports the tag table as CSV.
//
// Import only ever adds tags that are not already there: a shared list is a
// starting point, and re-importing it must never undo a local rename or
// description. That also makes the operation idempotent and safe to repeat.
func runTagFlags(g *meta.GlobalConfig, importFile, exportFile string, useGlobal bool) error {
	// A collection is the default target, because tags are per collection.
	var cfg interface {
		MergeTags([]meta.ImportedTag) meta.ImportResult
		Tagger() []meta.Tag
	}
	var save func() error
	var where string
	if useGlobal {
		cfg = g
		save = g.Save
		if p, err := meta.GlobalConfigPath(); err == nil {
			where = p
		}
	} else {
		dir, err := resolveDir(flag.Args(), g)
		if err != nil {
			return err
		}
		lib, err := app.OpenDirWith(dir, g)
		if err != nil {
			return err
		}
		cfg = &lib.Store.Config
		save = lib.Store.Save
		where = filepath.Join(lib.Dir, meta.StoreFile)
	}

	switch {
	case importFile != "":
		f, err := os.Open(importFile)
		if err != nil {
			return err
		}
		defer f.Close()
		tags, err := meta.ReadTagCSV(f)
		if err != nil {
			return fmt.Errorf("%s: %w", importFile, err)
		}
		res := cfg.MergeTags(tags)
		if err := save(); err != nil {
			return err
		}
		fmt.Printf("%s: %s\n", where, res.Summary())
		for _, name := range res.Added {
			fmt.Println("  +", name)
		}
		for _, name := range res.Invalid {
			fmt.Printf("  ! %q ignored: not a usable tag name\n", name)
		}
		return nil
	}

	// Export.
	var out *os.File
	if exportFile == "-" {
		out = os.Stdout
	} else {
		f, err := os.Create(exportFile)
		if err != nil {
			return err
		}
		defer f.Close()
		out = f
	}
	if err := meta.WriteTagCSV(out, cfg.Tagger()); err != nil {
		return err
	}
	if exportFile != "-" {
		fmt.Fprintf(os.Stderr, "wrote %s\n", exportFile)
	}
	return nil
}

// knownTags is the set of tag names a filter expression may use, for warning
// about typos.
func knownTags(lib *app.Library) func(string) bool {
	return func(name string) bool {
		_, ok := lib.Store.Config.Tag(name)
		return ok
	}
}

// printQuery prints the papers matching a query, then exits.
//
// The output is one file name per line and nothing else. A query is asked by a
// script that already knows what it wants to do with the file, and a table of
// extra columns only has to be cut away again; the metadata is one `--list`
// away for a human.
func printQuery(lib *app.Library, q app.Query, paths, count, noWrite bool) error {
	// Matching reads titles and tags, so anything the store lacks is filled in
	// first; otherwise a query would silently miss an unextracted paper.
	for _, p := range lib.Papers {
		if _, err := entryFor(lib, p, noWrite); err != nil {
			return err
		}
	}
	idx := q.Select(lib)
	if count {
		fmt.Println(len(idx))
		return nil
	}
	for _, i := range idx {
		p := lib.Papers[i]
		if paths {
			fmt.Println(filepath.Join(lib.Dir, p.Name))
			continue
		}
		fmt.Println(p.Name)
	}
	return nil
}

// entryFor returns a paper's metadata, extracting and storing it when the store
// has no title yet. With noWrite it extracts into a temporary entry instead, so
// a query never modifies the store.
func entryFor(lib *app.Library, p *app.Paper, noWrite bool) (*meta.Entry, error) {
	if e := p.Meta; e != nil && strings.TrimSpace(e.Title) != "" {
		return e, nil
	}
	if noWrite {
		res := extract.Extract(lib.Path(p.Name))
		return &meta.Entry{Title: res.TitleOr(p.Name), Author: res.Author, Abstract: res.Abstract}, nil
	}
	return lib.EnsureMeta(p, false)
}

// reorderArgs puts every flag before the positional arguments, so
// `non . --list` behaves like `non --list .`.
//
// The flag package stops at the first positional argument, which makes the
// trailing-flag form silently launch the TUI instead of listing. Reordering is
// safe because this program takes exactly one positional argument, and only
// flags are moved; a second positional is reported rather than guessed at.
func reorderArgs(args []string, isBool func(name string) bool) ([]string, error) {
	var flags, pos []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			// Keep the terminator: it tells flag.Parse that what follows is
			// positional even if it starts with a dash.
			flags = append(flags, "--")
			pos = append(pos, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			pos = append(pos, a)
			continue
		}
		flags = append(flags, a)
		name := strings.TrimLeft(a, "-")
		if strings.Contains(name, "=") || isBool(name) {
			continue
		}
		// A non-boolean flag consumes the next token as its value.
		if i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	if len(pos) > 1 {
		return nil, fmt.Errorf("unexpected argument %q (only one directory may be given)", pos[1])
	}
	return append(flags, pos...), nil
}

// isBoolFlag reports whether a flag is a boolean, so reorderArgs knows whether
// it takes a value.
func isBoolFlag(name string) bool {
	f := flag.CommandLine.Lookup(name)
	if f == nil {
		return false
	}
	b, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && b.IsBoolFlag()
}
