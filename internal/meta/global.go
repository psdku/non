package meta

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Tag names are deliberately narrow: letters, digits and underscores. A tag is
// typed in the filter expression language, where every other character is
// either an operator, a quote, or an escape — allowing them would mean asking
// the user to quote tag names to filter by them, which is not worth it.
//
// Unicode letters are allowed so a name can be written in one's own script, but
// punctuation and spaces are not. The underscore stands in for a space, which
// is the convention the rest of the file already uses.
var tagNameRE = regexp.MustCompile(`^[\p{L}\p{N}_]+$`)

// ErrTagName is returned for a tag name that is empty or has a character
// outside letters, digits and underscore.
var ErrTagName = errors.New("a tag name may only contain letters, digits and underscores")

// ValidTagName reports whether a tag name is acceptable.
func ValidTagName(name string) bool { return tagNameRE.MatchString(name) }

// SanitizeTagName turns arbitrary text into an acceptable tag name: spaces and
// punctuation become underscores, runs of underscores collapse, and leading or
// trailing underscores are dropped. It returns "" when nothing usable is left.
func SanitizeTagName(s string) string {
	var b strings.Builder
	lastUnderscore := false
	for _, r := range strings.TrimSpace(s) {
		switch {
		case r == '_' || r == '-' || r == '.' || r == ' ' || r == '/' || r == '\t':
			if b.Len() > 0 && !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		default:
			// Keep letters and digits, drop everything else (quotes, control
			// characters, operator characters).
			if tagNameRE.MatchString(string(r)) {
				b.WriteRune(r)
				lastUnderscore = false
			}
		}
	}
	return strings.Trim(b.String(), "_")
}

// CheckTagName returns ErrTagName-wrapped detail for a bad name.
func CheckTagName(name string) error {
	if name == "" {
		return errors.New("a tag needs a name")
	}
	if !ValidTagName(name) {
		return fmt.Errorf("%w: %q", ErrTagName, name)
	}
	return nil
}

// GlobalConfig is the per-user configuration, kept outside any collection so it
// can be read before a directory is chosen.
//
// It holds only what has to be known before opening a directory: the default
// directory, and the tools and tags a new or sparse collection inherits. The
// settings modal writes to the collection's .non.json, not here, except for the
// default directory which only exists here.
type GlobalConfig struct {
	// DefaultDir is opened when no directory argument is given. Empty means
	// "use the current directory".
	DefaultDir string `json:"default_dir,omitempty"`
	// Tools and TagTable are inherited by a collection that has none.
	Tools    []Tool `json:"tools,omitempty"`
	TagTable []Tag  `json:"tags,omitempty"`

	// Fields preserves unknown keys, as everywhere else.
	Fields map[string]any `json:"-"`

	path string
}

// GlobalConfigFile is the name of the user configuration file.
const GlobalConfigFile = "config.json"

// GlobalConfigPath returns the path of the user configuration file, honouring
// XDG_CONFIG_HOME and falling back to ~/.config.
func GlobalConfigPath() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "non", GlobalConfigFile), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "non", GlobalConfigFile), nil
}

// LoadGlobal reads the user configuration. A missing file is not an error: it
// yields an empty configuration, so a fresh install needs no setup.
func LoadGlobal() (*GlobalConfig, error) {
	path, err := GlobalConfigPath()
	if err != nil {
		return nil, err
	}
	return LoadGlobalAt(path)
}

// LoadGlobalAt reads the user configuration from an explicit path.
func LoadGlobalAt(path string) (*GlobalConfig, error) {
	g := &GlobalConfig{path: path}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return g, nil
		}
		return nil, err
	}
	if err := g.UnmarshalJSON(b); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	// UnmarshalJSON resets the struct, so restore the path it was read from;
	// otherwise Save would fall back to the default location and overwrite the
	// user's real config when someone loaded a different file.
	g.path = path
	return g, nil
}

// Path returns the file this configuration was read from.
func (g *GlobalConfig) Path() string { return g.path }

// UnmarshalJSON decodes the document, keeping unknown keys verbatim.
func (g *GlobalConfig) UnmarshalJSON(b []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*g = GlobalConfig{}
	for k, v := range raw {
		switch strings.ToLower(k) {
		case "default_dir", "dir", "directory":
			_ = json.Unmarshal(v, &g.DefaultDir)
		case "tools":
			_ = json.Unmarshal(v, &g.Tools)
		case "tags", "tag_table":
			_ = json.Unmarshal(v, &g.TagTable)
		default:
			var unknown any
			if json.Unmarshal(v, &unknown) == nil {
				if g.Fields == nil {
					g.Fields = map[string]any{}
				}
				g.Fields[k] = unknown
			}
		}
	}
	g.normalize()
	return nil
}

// MarshalJSON encodes the document, merging unknown keys.
func (g GlobalConfig) MarshalJSON() ([]byte, error) {
	m := map[string]any{}
	for k, v := range g.Fields {
		m[k] = v
	}
	if g.DefaultDir != "" {
		m["default_dir"] = g.DefaultDir
	} else {
		delete(m, "default_dir")
	}
	if len(g.Tools) > 0 {
		m["tools"] = g.Tools
	} else {
		delete(m, "tools")
	}
	if len(g.TagTable) > 0 {
		m["tags"] = g.TagTable
	} else {
		delete(m, "tags")
	}
	return json.Marshal(m)
}

// normalize trims the directory and drops unusable tag names.
func (g *GlobalConfig) normalize() {
	g.DefaultDir = strings.TrimSpace(g.DefaultDir)
	out := g.TagTable[:0]
	for _, t := range g.TagTable {
		t.Name = strings.TrimSpace(t.Name)
		if ValidTagName(t.Name) {
			out = append(out, t)
		}
	}
	g.TagTable = out
}

// Save writes the configuration, creating its directory if needed. A config
// with no path is refused rather than written to a guessed location: silently
// writing the user's real config from a struct that was never loaded is how a
// test or a --set-dir on a copy could clobber it.
func (g *GlobalConfig) Save() error {
	if g.path == "" {
		return errors.New("global config has no path; use LoadGlobal or set one")
	}
	if err := os.MkdirAll(filepath.Dir(g.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(g.path), ".non-global-*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, g.path)
}

// Tag reports whether the global table has a tag, and returns it.
func (g *GlobalConfig) Tag(name string) (Tag, bool) {
	for _, t := range g.TagTable {
		if strings.EqualFold(t.Name, name) {
			return t, true
		}
	}
	return Tag{}, false
}

// ---- tag import / export -------------------------------------------------

// ImportedTag is one row of an import file.
type ImportedTag struct {
	Name        string
	Description string
}

// ImportResult reports what an import did, so the caller can print it.
type ImportResult struct {
	Added    []string // tags that did not exist
	Skipped  []string // tags that already existed
	Invalid  []string // rows whose name could not be used
	Imported int      // rows read
}

// Summary is a one-line description of the import.
func (r ImportResult) Summary() string {
	if r.Imported == 0 {
		return "no tags to import"
	}
	var parts []string
	if len(r.Added) > 0 {
		parts = append(parts, fmt.Sprintf("added %d", len(r.Added)))
	}
	if len(r.Skipped) > 0 {
		parts = append(parts, fmt.Sprintf("skipped %d existing", len(r.Skipped)))
	}
	if len(r.Invalid) > 0 {
		parts = append(parts, fmt.Sprintf("ignored %d invalid", len(r.Invalid)))
	}
	return strings.Join(parts, ", ")
}

// ReadTagCSV parses a tag file. The first row may be a header ("name" and
// optionally "description", case-insensitively); one column per row is fine
// too, in which case it is a name.
func ReadTagCSV(r io.Reader) ([]ImportedTag, error) {
	cr := csv.NewReader(r)
	// Blank lines and trailing commas are common in hand-edited files.
	cr.FieldsPerRecord = -1
	cr.TrimLeadingSpace = true
	rows, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}
	var out []ImportedTag
	for i, row := range rows {
		if len(row) == 0 {
			continue
		}
		name := strings.TrimSpace(row[0])
		if name == "" {
			continue
		}
		desc := ""
		if len(row) > 1 {
			desc = strings.TrimSpace(row[1])
		}
		if i == 0 && strings.EqualFold(name, "name") {
			continue // header
		}
		out = append(out, ImportedTag{Name: name, Description: desc})
	}
	return out, nil
}

// WriteTagCSV writes the table as CSV with a header row.
func WriteTagCSV(w io.Writer, tags []Tag) error {
	cw := csv.NewWriter(w)
	if err := cw.Write([]string{"name", "description"}); err != nil {
		return err
	}
	sorted := append([]Tag(nil), tags...)
	sort.Slice(sorted, func(i, j int) bool {
		return strings.ToLower(sorted[i].Name) < strings.ToLower(sorted[j].Name)
	})
	for _, t := range sorted {
		if err := cw.Write([]string{t.Name, t.Description}); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// MergeTags adds every tag that does not already exist, by name, and reports
// what happened. The name is sanitized first: an import is not the place to
// teach the naming rules, so "KV cache" quietly becomes "KV_cache".
//
// An existing tag is left completely alone, including its description: the
// point of "only import non-existing" is that a local edit is never lost to a
// re-import of a shared list.
func (c *Config) MergeTags(tags []ImportedTag) ImportResult {
	var res ImportResult
	for _, t := range tags {
		name := SanitizeTagName(t.Name)
		if name == "" {
			res.Invalid = append(res.Invalid, t.Name)
			continue
		}
		res.Imported++
		if _, ok := c.Tag(name); ok {
			res.Skipped = append(res.Skipped, name)
			continue
		}
		c.TagTable = append(c.TagTable, Tag{Name: name, Description: strings.TrimSpace(t.Description)})
		res.Added = append(res.Added, name)
	}
	sort.Strings(res.Added)
	sort.Strings(res.Skipped)
	sort.Strings(res.Invalid)
	return res
}

// Tagger is the tag table, named so a collection and the user config can be
// handled through one interface by the import and export flags.
func (c *Config) Tagger() []Tag { return c.TagTable }

// Tagger implements the same interface as Config.
func (g *GlobalConfig) Tagger() []Tag { return g.TagTable }

// MergeTags is the same for the user configuration.
func (g *GlobalConfig) MergeTags(tags []ImportedTag) ImportResult {
	c := &Config{TagTable: g.TagTable}
	res := c.MergeTags(tags)
	g.TagTable = c.TagTable
	return res
}

// Inherit fills in the parts of a collection's configuration it does not define
// from the user configuration.
//
// Only empty sections are filled: a collection with its own tools keeps them,
// and only a collection with no tag table at all adopts the global one. An
// empty tag table is a real choice ("this collection uses no tags"), but it is
// indistinguishable from "never configured", and adoption is the more useful
// reading for a fresh directory. Once a tag is added locally the table is
// non-empty, so the inheritance stops on its own.
func (s *Store) Inherit(g *GlobalConfig) {
	if g == nil {
		return
	}
	if len(s.Config.Tools) == 0 && len(g.Tools) > 0 {
		s.Config.Tools = append([]Tool(nil), g.Tools...)
		if s.Config.DefaultTool == "" && len(s.Config.Tools) > 0 {
			s.Config.DefaultTool = s.Config.Tools[0].Name
		}
	}
	if len(s.Config.TagTable) == 0 && len(g.TagTable) > 0 {
		s.Config.TagTable = append([]Tag(nil), g.TagTable...)
	}
}
