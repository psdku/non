package app

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"non/internal/extract"
	"non/internal/meta"
)

// Paper pairs a PDF on disk with its metadata.
type Paper struct {
	Name string // file name, relative to Dir
	Meta *meta.Entry
}

// Display returns the best available title.
func (p *Paper) Display() string {
	if p.Meta != nil {
		if t := strings.TrimSpace(p.Meta.Title); t != "" {
			return t
		}
	}
	return extract.TitleFromFilename(p.Name)
}

// HasMeta reports whether the entry carries anything worth showing.
func (p *Paper) HasMeta() bool {
	if p.Meta == nil {
		return false
	}
	m := p.Meta
	return m.Title != "" || m.Author != "" || len(m.Tags) > 0 || m.Abstract != "" || m.Comment != ""
}

// Library is the directory being browsed.
type Library struct {
	Dir    string
	Store  *meta.Store
	Papers []*Paper
}

// OpenDir scans dir for PDFs and loads its metadata store.
func OpenDir(dir string) (*Library, error) { return OpenDirWith(dir, nil) }

// OpenDirWith opens a directory and lets the user configuration fill in what
// the collection does not define.
//
// A collection that has no tools or no tag table inherits them from the global
// config. That is what makes "open any directory" pleasant: you get your tools
// and your tags without copying a config into every folder. A collection that
// defines its own wins, so a carefully curated tag table is never overwritten
// by a change to the global default.
func OpenDirWith(dir string, global *meta.GlobalConfig) (*Library, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	store, err := meta.Load(abs)
	if err != nil {
		return nil, err
	}
	if global != nil {
		store.Inherit(global)
	}
	lib := &Library{Dir: abs, Store: store}
	if err := lib.Rescan(); err != nil {
		return nil, err
	}
	return lib, nil
}

// Rescan re-reads the directory listing, keeping existing metadata objects.
func (l *Library) Rescan() error {
	entries, err := os.ReadDir(l.Dir)
	if err != nil {
		return err
	}
	byName := make(map[string]*Paper, len(l.Papers))
	for _, p := range l.Papers {
		byName[p.Name] = p
	}
	var papers []*Paper
	for _, de := range entries {
		if de.IsDir() || !strings.EqualFold(filepath.Ext(de.Name()), ".pdf") {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		p := byName[de.Name()]
		if p == nil {
			p = &Paper{Name: de.Name()}
		}
		_ = info // the file exists and is a regular PDF
		if m, ok := l.Store.Lookup(de.Name()); ok {
			p.Meta = m
		} else {
			p.Meta = nil
		}
		papers = append(papers, p)
	}
	sort.Slice(papers, func(i, j int) bool {
		return strings.ToLower(papers[i].Name) < strings.ToLower(papers[j].Name)
	})
	l.Papers = papers
	return nil
}

// Paper returns the paper with the given name.
func (l *Library) Paper(name string) *Paper {
	for _, p := range l.Papers {
		if p.Name == name {
			return p
		}
	}
	return nil
}

// Path returns the absolute path of a paper.
func (l *Library) Path(name string) string { return filepath.Join(l.Dir, name) }

// EnsureMeta returns metadata for p, extracting it from the PDF and persisting
// it the first time the file is seen. Missing titles are filled in; other
// fields are only set when the entry is still marked automatic.
func (l *Library) EnsureMeta(p *Paper, force bool) (*meta.Entry, error) {
	if p.Meta != nil && !force && strings.TrimSpace(p.Meta.Title) != "" {
		return p.Meta, nil
	}
	res := extract.Extract(l.Path(p.Name))
	e := &meta.Entry{}
	if p.Meta != nil {
		e = p.Meta.Clone()
	}
	auto := p.Meta == nil || p.Meta.Auto
	if strings.TrimSpace(e.Title) == "" {
		e.Title = res.TitleOr(p.Name)
	}
	if auto {
		if e.Author == "" {
			e.Author = authorFrom(res)
		}
		if strings.TrimSpace(e.Abstract) == "" {
			e.Abstract = res.Abstract
		}
	}
	e.Auto = p.Meta == nil || p.Meta.Auto
	l.Store.Set(p.Name, e)
	p.Meta = e
	return e, l.Store.Save()
}

// authorFrom returns the author recorded in the PDF, if any.
func authorFrom(res extract.Result) string {
	a := strings.TrimSpace(res.Author)
	if strings.ContainsAny(a, "\n\r") {
		a = strings.Join(strings.Fields(a), " ")
	}
	return a
}

// SaveMeta stores e for the named paper.
func (l *Library) SaveMeta(name string, e *meta.Entry) error {
	l.Store.Set(name, e)
	if p := l.Paper(name); p != nil {
		p.Meta = e
	}
	return l.Store.Save()
}

// DeleteMeta removes the metadata for a paper.
func (l *Library) DeleteMeta(name string) error {
	l.Store.Delete(name)
	if p := l.Paper(name); p != nil {
		p.Meta = nil
	}
	return l.Store.Save()
}

// RenamePaper renames a file on disk and moves its metadata along.
func (l *Library) RenamePaper(oldName, newName string) (string, error) {
	final, err := SafeRename(l.Dir, oldName, newName)
	if err != nil {
		return "", err
	}
	if final != oldName {
		l.Store.Rename(oldName, final)
		if err := l.Store.Save(); err != nil {
			return final, err
		}
		if err := l.Rescan(); err != nil {
			return final, err
		}
	}
	return final, nil
}

// AddOpener installs a program as a gui tool and makes it the default.
func (l *Library) AddOpener(command string) {
	command = strings.TrimSpace(command)
	if command == "" {
		return
	}
	name := toolName(command)
	_ = l.Store.Config.AddTool(meta.Tool{Name: name, Command: command, Type: meta.ToolGUI})
	l.Store.Config.DefaultTool = name
}

// toolName derives a display name from a command line.
func toolName(command string) string {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return "tool"
	}
	name := fields[0]
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSuffix(name, ".exe")
	if name == "" {
		return "tool"
	}
	return name
}
