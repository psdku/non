// Package meta implements the on-disk metadata store used by non.
//
// The store is a single JSON file (.non.json) in the directory being
// browsed:
//
//	{
//	  "version": 1,
//	  "config": {"opener": "firefox"},
//	  "entries": {
//	    "1706.03762v7.pdf": {
//	      "title": "Attention Is All You Need",
//	      "author": "Ashish Vaswani, Noam Shazeer",
//	      "tags": ["transformer", "attention"],
//	      "abstract": "The dominant sequence transduction models ...",
//	      "auto": true
//	    }
//	  }
//	}
//
// Unknown keys inside an entry are preserved verbatim (see Entry.Fields), so
// the format is extensible without changing this package.
package meta

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// StoreFile is the name of the metadata file kept in the browsed directory.
const StoreFile = ".non.json"

// Entry is the metadata for one PDF.
type Entry struct {
	Title    string   `json:"title"`
	Author   string   `json:"author,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Abstract string   `json:"abstract,omitempty"`
	Comment  string   `json:"comment,omitempty"`

	// Auto records that the entry was filled in automatically from the PDF
	// contents and has never been edited by hand.
	Auto bool `json:"auto,omitempty"`

	// Fields holds any additional, user supplied fields (e.g. "doi", "year").
	// Values are kept as raw JSON so that numbers, arrays, etc. round-trip.
	Fields map[string]json.RawMessage `json:"-"`
}

// Store is the whole .non.json document.
type Store struct {
	Version int               `json:"version"`
	Entries map[string]*Entry `json:"entries"`
	Config  Config            `json:"config,omitzero"`

	// Fields preserves unknown top level keys (same rule as Entry.Fields), so
	// a document written by a newer schema of the same version survives a
	// round trip through this build.
	Fields map[string]json.RawMessage `json:"-"`

	path string // absolute path of the backing file
}

// MarshalJSON encodes the store, merging unknown top level keys.
func (s Store) MarshalJSON() ([]byte, error) {
	m := make(map[string]json.RawMessage, len(s.Fields)+3)
	maps.Copy(m, s.Fields)
	version, err := json.Marshal(s.Version)
	if err != nil {
		return nil, err
	}
	m["version"] = version
	entries := s.Entries
	if entries == nil {
		entries = map[string]*Entry{}
	}
	b, err := json.Marshal(entries)
	if err != nil {
		return nil, err
	}
	m["entries"] = b
	// An empty config is omitted, so a store with only entries stays small.
	if !s.Config.empty() || len(s.Config.Fields) > 0 {
		b, err := json.Marshal(s.Config)
		if err != nil {
			return nil, err
		}
		m["config"] = b
	} else {
		delete(m, "config")
	}
	return json.Marshal(m)
}

// UnmarshalJSON decodes the store, keeping unknown top level keys.
func (s *Store) UnmarshalJSON(b []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*s = Store{Entries: map[string]*Entry{}}
	for k, v := range raw {
		switch k {
		case "version":
			if err := json.Unmarshal(v, &s.Version); err != nil {
				return err
			}
		case "entries":
			if err := json.Unmarshal(v, &s.Entries); err != nil {
				return err
			}
		case "config":
			if err := json.Unmarshal(v, &s.Config); err != nil {
				return err
			}
		default:
			if s.Fields == nil {
				s.Fields = map[string]json.RawMessage{}
			}
			s.Fields[k] = v
		}
	}
	if s.Entries == nil {
		s.Entries = map[string]*Entry{}
	}
	return nil
}

// New returns an empty store at the current schema version.
func New() *Store {
	return &Store{Version: Version, Entries: map[string]*Entry{}}
}

// Path returns the path the store was loaded from / saved to.
func (s *Store) Path() string { return s.path }

// Load reads the store in dir, returning an empty store if none exists.
func Load(dir string) (*Store, error) {
	p := filepath.Join(dir, StoreFile)
	s := New()
	s.path = p
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(b, s); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	if s.Entries == nil {
		s.Entries = map[string]*Entry{}
	}
	// A legacy flat "config" map (or an "opener" key) is folded into the
	// structured config here, so callers only ever see the current shape.
	s.Config.migrate()
	// Repair what can be repaired without losing information: duplicate tag
	// rows, and entry tags that differ only in case. Unknown tags are left for
	// Store.Problems to report, because deleting one would lose intent.
	s.Normalize()
	// The schema version decides whether the document can be used at all; a
	// file from a newer build is refused rather than rewritten.
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	// The path field does not survive unmarshalling into a fresh struct
	// when callers pass a partially initialized store, so pin it here.
	s.path = p
	return s, nil
}

// LoadAt opens the store stored at an explicit path.
func LoadAt(path string) (*Store, error) {
	s, err := Load(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	s.path = path
	return s, nil
}

// SaveTo sets the backing path and saves. It exists so a store built in memory
// (or moved to another directory) can be written without reaching into path.
func (s *Store) SaveTo(dir string) error {
	s.path = filepath.Join(dir, StoreFile)
	return s.Save()
}

// Save writes the store atomically.
func (s *Store) Save() error {
	if s.path == "" {
		return errors.New("store has no path")
	}
	// Canonicalise on the way out too: a caller that built an entry by hand
	// still gets one spelling per tag, and a table that was hand-edited into
	// duplicates is merged by the first write.
	s.Config.canonicalizeTable()
	s.canonicalizeTags()
	// Refuse to write a document this build does not understand, even if the
	// caller constructed the store by hand.
	if err := s.migrate(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".non-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		return err
	}
	return os.Rename(name, s.path)
}

// Get returns the entry for name, creating an empty one if needed.
func (s *Store) Get(name string) *Entry {
	if e, ok := s.Entries[name]; ok && e != nil {
		return e
	}
	e := &Entry{}
	s.Entries[name] = e
	return e
}

// Lookup returns the entry for name if it exists.
func (s *Store) Lookup(name string) (*Entry, bool) {
	e, ok := s.Entries[name]
	return e, ok && e != nil
}

// Set stores e under name.
func (s *Store) Set(name string, e *Entry) { s.Entries[name] = e }

// Delete removes name from the store.
func (s *Store) Delete(name string) { delete(s.Entries, name) }

// Rename moves the metadata of oldName to newName.
func (s *Store) Rename(oldName, newName string) {
	e, ok := s.Entries[oldName]
	if !ok {
		return
	}
	delete(s.Entries, oldName)
	s.Entries[newName] = e
}

// Prune drops entries whose PDF no longer exists in dir.
func (s *Store) Prune(dir string) (removed []string) {
	for name := range s.Entries {
		if fi, err := os.Stat(filepath.Join(dir, name)); err != nil || fi.IsDir() {
			delete(s.Entries, name)
			removed = append(removed, name)
		}
	}
	sort.Strings(removed)
	return removed
}

// Names returns the entry names sorted.
func (s *Store) Names() []string {
	out := make([]string, 0, len(s.Entries))
	for n := range s.Entries {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// FieldString returns the string value of an extra field.
func (e *Entry) FieldString(key string) string {
	v, ok := e.field(key)
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(v, &s); err == nil {
		return s
	}
	return strings.Trim(string(v), `"`)
}

func (e *Entry) field(key string) (json.RawMessage, bool) {
	if e.Fields == nil {
		return nil, false
	}
	if v, ok := e.Fields[key]; ok {
		return v, true
	}
	for k, v := range e.Fields {
		if strings.EqualFold(k, key) {
			return v, true
		}
	}
	return nil, false
}

// SetField sets a string valued extra field.
func (e *Entry) SetField(key, value string) {
	if e.Fields == nil {
		e.Fields = map[string]json.RawMessage{}
	}
	b, _ := json.Marshal(value)
	e.Fields[key] = b
}

// SetRawField stores an already encoded JSON value.
func (e *Entry) SetRawField(key string, value json.RawMessage) {
	if e.Fields == nil {
		e.Fields = map[string]json.RawMessage{}
	}
	e.Fields[key] = value
}

// DeleteField removes an extra field.
func (e *Entry) DeleteField(key string) {
	if e.Fields == nil {
		return
	}
	for k := range e.Fields {
		if strings.EqualFold(k, key) {
			delete(e.Fields, k)
		}
	}
	if len(e.Fields) == 0 {
		e.Fields = nil
	}
}

// HasField reports whether an extra field exists (case-insensitively).
func (e *Entry) HasField(key string) bool {
	_, ok := e.field(key)
	return ok
}

// knownKeys are the keys handled by the typed fields.
var knownKeys = map[string]bool{
	"title": true, "author": true, "authors": true, "tags": true,
	"abstract": true, "comment": true, "auto": true,
}

// MarshalJSON encodes an entry, merging the extra fields.
func (e Entry) MarshalJSON() ([]byte, error) {
	m := make(map[string]json.RawMessage, len(e.Fields)+6)
	for k, v := range e.Fields {
		if knownKeys[strings.ToLower(k)] {
			continue
		}
		m[k] = v
	}
	put := func(k string, v any) {
		b, err := json.Marshal(v)
		if err != nil {
			return
		}
		m[k] = b
	}
	put("title", e.Title)
	if e.Author != "" {
		put("author", e.Author)
	}
	if len(e.Tags) > 0 {
		put("tags", e.Tags)
	}
	if e.Abstract != "" {
		put("abstract", e.Abstract)
	}
	if e.Comment != "" {
		put("comment", e.Comment)
	}
	if e.Auto {
		put("auto", true)
	}
	return json.Marshal(m)
}

// UnmarshalJSON decodes an entry, keeping unknown keys in Fields.
func (e *Entry) UnmarshalJSON(b []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*e = Entry{Fields: map[string]json.RawMessage{}}
	for k, v := range raw {
		switch strings.ToLower(k) {
		case "title":
			_ = json.Unmarshal(v, &e.Title)
		case "author", "authors":
			if err := json.Unmarshal(v, &e.Author); err != nil {
				var list []string
				if json.Unmarshal(v, &list) == nil {
					e.Author = strings.Join(list, ", ")
				}
			}
		case "tags":
			_ = json.Unmarshal(v, &e.Tags)
		case "abstract":
			_ = json.Unmarshal(v, &e.Abstract)
		case "comment":
			_ = json.Unmarshal(v, &e.Comment)
		case "auto":
			_ = json.Unmarshal(v, &e.Auto)
		default:
			e.Fields[k] = v
		}
	}
	if len(e.Fields) == 0 {
		e.Fields = nil
	}
	return nil
}

// NormalizeTags splits a comma separated tag list, trimming blanks.
func NormalizeTags(tags []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range tags {
		for part := range strings.SplitSeq(t, ",") {
			p := strings.TrimSpace(part)
			if p == "" {
				continue
			}
			k := strings.ToLower(p)
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, p)
		}
	}
	return out
}

// Clone returns a deep copy of the entry.
func (e *Entry) Clone() *Entry {
	c := *e
	c.Tags = append([]string(nil), e.Tags...)
	if e.Fields != nil {
		c.Fields = make(map[string]json.RawMessage, len(e.Fields))
		for k, v := range e.Fields {
			c.Fields[k] = append(json.RawMessage(nil), v...)
		}
	}
	return &c
}

// Equal compares two entries semantically (ignoring Auto and field order).
func (e *Entry) Equal(o *Entry) bool {
	if e == nil || o == nil {
		return e == o
	}
	a, err1 := json.Marshal(e)
	b, err2 := json.Marshal(o)
	if err1 != nil || err2 != nil {
		return false
	}
	return string(a) == string(b)
}
