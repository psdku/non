package meta

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"runtime"
	"sort"
	"strings"
)

// Tool types. A "gui" tool is started and left running; the terminal is not
// handed over. A "tui" tool takes over the terminal until it exits.
const (
	ToolGUI = "gui"
	ToolTUI = "tui"
)

// Tool is one entry of the "open with" list configured in the settings.
type Tool struct {
	Name    string `json:"name"`
	Command string `json:"command"`
	Type    string `json:"type,omitempty"` // gui (default) or tui
}

// IsTUI reports whether the tool takes over the terminal.
func (t Tool) IsTUI() bool { return strings.EqualFold(t.Type, ToolTUI) }

// NormalizeToolType maps loose user input onto a known type.
func NormalizeToolType(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "tui", "terminal", "cli", "text":
		return ToolTUI
	case "", "gui", "graphical", "desktop", "window":
		return ToolGUI
	}
	return ToolGUI
}

// Tag is an entry of the global tag table. Name is what appears on a paper;
// Description is what makes the table worth keeping.
type Tag struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Fields preserves unknown keys so the table stays extensible.
	Fields map[string]json.RawMessage `json:"-"`
}

// MarshalJSON encodes a tag, merging any extra fields.
func (t Tag) MarshalJSON() ([]byte, error) {
	m := make(map[string]json.RawMessage, len(t.Fields)+2)
	maps.Copy(m, t.Fields)
	name, err := json.Marshal(t.Name)
	if err != nil {
		return nil, err
	}
	m["name"] = name
	if t.Description != "" {
		d, err := json.Marshal(t.Description)
		if err != nil {
			return nil, err
		}
		m["description"] = d
	}
	return json.Marshal(m)
}

// UnmarshalJSON decodes a tag; a bare string is accepted too, because the
// first versions of the format stored tags as plain strings.
func (t *Tag) UnmarshalJSON(b []byte) error {
	var name string
	if err := json.Unmarshal(b, &name); err == nil {
		*t = Tag{Name: name}
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	*t = Tag{}
	for k, v := range raw {
		switch strings.ToLower(k) {
		case "name":
			_ = json.Unmarshal(v, &t.Name)
		case "description", "desc":
			_ = json.Unmarshal(v, &t.Description)
		default:
			if t.Fields == nil {
				t.Fields = map[string]json.RawMessage{}
			}
			t.Fields[k] = v
		}
	}
	if len(t.Fields) == 0 {
		t.Fields = nil
	}
	return nil
}

// Config is the global settings document. The zero value is valid and means
// "edit metadata in the built-in form, search file names only, no tools, no
// tags".
type Config struct {
	// UseEditor switches metadata editing to $EDITOR. It defaults to off, so a
	// fresh install edits metadata in the built-in form; the built-in form is
	// the only editor that can enforce the tag table.
	//
	// The JSON key is "open_editor" because that is the wording used in the
	// settings modal; the older "inline_editor" key is still read for
	// compatibility (see UnmarshalJSON).
	UseEditor bool `json:"open_editor,omitempty"`
	// SearchMeta makes fuzzy search look inside metadata by default.
	SearchMeta bool `json:"search_meta,omitempty"`
	// Tools is the predefined list of programs offered by "open with".
	Tools []Tool `json:"tools,omitempty"`
	// DefaultTool names the tool used by a plain "o".
	DefaultTool string `json:"default_tool,omitempty"`
	// TagTable is the global table of tags and their descriptions.
	TagTable []Tag `json:"tags,omitempty"`
	// LegacyOpener is the single command used before tools existed. It is
	// migrated into Tools on load and then cleared.
	LegacyOpener string `json:"opener,omitempty"`

	// Fields preserves unknown keys written by other tools.
	Fields map[string]json.RawMessage `json:"-"`
}

// MarshalJSON encodes the config, merging unknown keys.
func (c Config) MarshalJSON() ([]byte, error) {
	m := make(map[string]json.RawMessage, len(c.Fields)+6)
	maps.Copy(m, c.Fields)
	put := func(k string, v any, skip bool) {
		if skip {
			delete(m, k)
			return
		}
		b, err := json.Marshal(v)
		if err != nil {
			return
		}
		m[k] = b
	}
	put("open_editor", c.UseEditor, !c.UseEditor)
	put("search_meta", c.SearchMeta, !c.SearchMeta)
	put("tools", c.Tools, len(c.Tools) == 0)
	put("default_tool", c.DefaultTool, c.DefaultTool == "")
	put("tags", c.TagTable, len(c.TagTable) == 0)
	// The legacy key is never written back.
	delete(m, "opener")
	return json.Marshal(m)
}

// UnmarshalJSON decodes both the current object form and the older form where
// "config" was a flat map of strings.
func (c *Config) UnmarshalJSON(b []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	// Older stores kept "config" as a flat map of strings. Recognise that
	// shape first so its keys are not mistaken for settings.
	if isFlatStringMap(raw) {
		legacy := Config{}
		var flat map[string]string
		if err := json.Unmarshal(b, &flat); err == nil {
			if v := strings.TrimSpace(flat["opener"]); v != "" {
				legacy.LegacyOpener = v
			}
			delete(flat, "opener")
			for k, v := range flat {
				enc, err := json.Marshal(v)
				if err != nil {
					continue
				}
				if legacy.Fields == nil {
					legacy.Fields = map[string]json.RawMessage{}
				}
				legacy.Fields[k] = enc
			}
			if len(legacy.Fields) == 0 {
				legacy.Fields = nil
			}
		}
		legacy.migrate()
		*c = legacy
		return nil
	}
	*c = Config{}
	for k, v := range raw {
		switch strings.ToLower(k) {
		case "open_editor":
			_ = json.Unmarshal(v, &c.UseEditor)
		case "inline_editor":
			// An early draft wrote the inverse under this name.
			var inline bool
			if json.Unmarshal(v, &inline) == nil {
				c.UseEditor = !inline
			}
		case "search_meta", "search_metadata":
			_ = json.Unmarshal(v, &c.SearchMeta)
		case "tools":
			_ = json.Unmarshal(v, &c.Tools)
		case "default_tool":
			_ = json.Unmarshal(v, &c.DefaultTool)
		case "tags", "tag_table":
			_ = json.Unmarshal(v, &c.TagTable)
		case "opener":
			// Either the legacy string or a tool object.
			var s string
			if json.Unmarshal(v, &s) == nil {
				c.LegacyOpener = s
				continue
			}
			var t Tool
			if json.Unmarshal(v, &t) == nil && t.Command != "" {
				c.Tools = append(c.Tools, t)
			}
		default:
			if c.Fields == nil {
				c.Fields = map[string]json.RawMessage{}
			}
			c.Fields[k] = v
		}
	}
	if len(c.Fields) == 0 {
		c.Fields = nil
	}
	c.migrate()
	return nil
}

// isFlatStringMap reports whether every value of the object is a JSON string.
// The current config always contains at least one bool, array or object, so
// this unambiguously identifies the legacy form.
func isFlatStringMap(raw map[string]json.RawMessage) bool {
	for _, v := range raw {
		t := bytes.TrimSpace(v)
		if len(t) == 0 || t[0] != '"' {
			return false
		}
	}
	return len(raw) > 0
}

// migrate turns a legacy single opener into a tool entry.
func (c *Config) migrate() {
	if strings.TrimSpace(c.LegacyOpener) == "" {
		c.LegacyOpener = ""
		return
	}
	cmd := strings.TrimSpace(c.LegacyOpener)
	c.LegacyOpener = ""
	if c.findTool(cmd) != nil {
		return
	}
	c.Tools = append(c.Tools, Tool{
		Name:    toolNameFromCommand(cmd),
		Command: cmd,
		Type:    ToolGUI,
	})
	if c.DefaultTool == "" {
		c.DefaultTool = toolNameFromCommand(cmd)
	}
}

// toolNameFromCommand derives a short display name from a command line.
func toolNameFromCommand(cmd string) string {
	fields := strings.Fields(cmd)
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

// DefaultTools is the tool list used when the config has none: a sensible
// platform opener plus nothing else.
func DefaultTools() []Tool {
	cmd := platformOpener()
	if cmd == "" {
		return nil
	}
	return []Tool{{Name: toolNameFromCommand(cmd), Command: cmd, Type: ToolGUI}}
}

// --- tools ----------------------------------------------------------------

func (c *Config) findTool(name string) *Tool {
	for i := range c.Tools {
		if strings.EqualFold(c.Tools[i].Name, name) {
			return &c.Tools[i]
		}
	}
	return nil
}

// Tool looks a tool up by name (case-insensitively).
func (c *Config) Tool(name string) (Tool, bool) {
	if t := c.findTool(name); t != nil {
		return *t, true
	}
	return Tool{}, false
}

// AddTool appends or replaces a tool by name.
func (c *Config) AddTool(t Tool) error {
	if strings.TrimSpace(t.Name) == "" {
		return ErrToolName
	}
	if strings.TrimSpace(t.Command) == "" {
		return ErrToolCommand
	}
	t.Name = strings.TrimSpace(t.Name)
	t.Command = strings.TrimSpace(t.Command)
	t.Type = NormalizeToolType(t.Type)
	if existing := c.findTool(t.Name); existing != nil {
		*existing = t
		return nil
	}
	c.Tools = append(c.Tools, t)
	if c.DefaultTool == "" {
		c.DefaultTool = t.Name
	}
	return nil
}

// RemoveTool deletes a tool by name and fixes up the default.
func (c *Config) RemoveTool(name string) {
	out := c.Tools[:0]
	for _, t := range c.Tools {
		if strings.EqualFold(t.Name, name) {
			continue
		}
		out = append(out, t)
	}
	c.Tools = out
	if strings.EqualFold(c.DefaultTool, name) {
		c.DefaultTool = ""
		if len(c.Tools) > 0 {
			c.DefaultTool = c.Tools[0].Name
		}
	}
}

// EffectiveTools returns the configured tools, or the platform default when
// none are configured.
func (c *Config) EffectiveTools() []Tool {
	if len(c.Tools) > 0 {
		return c.Tools
	}
	return DefaultTools()
}

// EffectiveDefault returns the tool a plain "o" should use.
func (c *Config) EffectiveDefault() (Tool, bool) {
	tools := c.EffectiveTools()
	if len(tools) == 0 {
		return Tool{}, false
	}
	if t := c.findTool(c.DefaultTool); t != nil {
		return *t, true
	}
	return tools[0], true
}

// --- tags -----------------------------------------------------------------

// Tags returns the global tag table, sorted by name. Tags that are used by a
// paper but missing from the table are included, so the table always covers
// what is displayed.
func (c *Config) Tags() []Tag {
	out := append([]Tag(nil), c.TagTable...)
	sort.SliceStable(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// TagNames returns the names of the global tag table.
func (c *Config) TagNames() []string {
	tags := c.Tags()
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		out = append(out, t.Name)
	}
	return out
}

// Tag looks a tag up, case-insensitively.
func (c *Config) Tag(name string) (Tag, bool) {
	for _, t := range c.TagTable {
		if strings.EqualFold(t.Name, name) {
			return t, true
		}
	}
	return Tag{}, false
}

// CanonicalTag returns the table's spelling of a tag name, or the given name
// when the tag is unknown.
func (c *Config) CanonicalTag(name string) string {
	if t, ok := c.Tag(name); ok {
		return t.Name
	}
	return name
}

// EnsureTag adds a tag to the table if it is missing and returns its canonical
// name. Existing tags keep their description.
func (c *Config) EnsureTag(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if !ValidTagName(name) {
		return "", false
	}
	if t, ok := c.Tag(name); ok {
		return t.Name, false
	}
	c.TagTable = append(c.TagTable, Tag{Name: name})
	return name, true
}

// SetTag adds or updates a tag. When the name changed, all papers using the
// old name are updated by Store.RenameTag, which has the entries.
func (c *Config) SetTag(index int, t Tag) {
	t.Name = strings.TrimSpace(t.Name)
	t.Description = strings.TrimSpace(t.Description)
	if !ValidTagName(t.Name) {
		return
	}
	// A name is unique regardless of case, so adding or renaming onto an
	// existing spelling is refused. Without this the table accumulates rows the
	// user cannot tell apart, which is exactly the corruption Load repairs.
	for i, existing := range c.TagTable {
		if i == index {
			continue
		}
		if strings.EqualFold(existing.Name, t.Name) {
			return
		}
	}
	if index < 0 || index >= len(c.TagTable) {
		c.TagTable = append(c.TagTable, t)
		return
	}
	c.TagTable[index] = t
}

// RemoveTag deletes a tag from the table by name.
func (c *Config) RemoveTag(name string) {
	out := c.TagTable[:0]
	for _, t := range c.TagTable {
		if strings.EqualFold(t.Name, name) {
			continue
		}
		out = append(out, t)
	}
	c.TagTable = out
}

// DescribeTag returns the description of a tag name.
func (c *Config) DescribeTag(name string) string {
	if t, ok := c.Tag(name); ok {
		return t.Description
	}
	return ""
}

// TagsInUse counts how many entries use each tag, canonicalised.
func (s *Store) TagsInUse() map[string]int {
	counts := map[string]int{}
	for _, e := range s.Entries {
		for _, t := range e.Tags {
			counts[s.Config.CanonicalTag(t)]++
		}
	}
	return counts
}

// SetTag saves a tag and renames it across every entry when the name changed.
func (s *Store) SetTag(index int, t Tag) {
	old := ""
	if index >= 0 && index < len(s.Config.TagTable) {
		old = s.Config.TagTable[index].Name
	}
	s.Config.SetTag(index, t)
	if old != "" && !strings.EqualFold(old, t.Name) && t.Name != "" {
		s.renameTagInEntries(old, t.Name)
	}
}

// RenameTag renames a tag in the table and in every entry.
func (s *Store) RenameTag(old, new string) {
	new = strings.TrimSpace(new)
	if new == "" || strings.EqualFold(old, new) {
		return
	}
	if idx := s.tagIndex(old); idx >= 0 {
		s.Config.TagTable[idx].Name = new
	} else {
		s.Config.EnsureTag(new)
	}
	s.renameTagInEntries(old, new)
}

// DeleteTag removes a tag from the table and from every entry.
func (s *Store) DeleteTag(name string) {
	s.Config.RemoveTag(name)
	for _, e := range s.Entries {
		e.Tags = removeTag(e.Tags, name)
	}
}

func (s *Store) tagIndex(name string) int {
	for i, t := range s.Config.TagTable {
		if strings.EqualFold(t.Name, name) {
			return i
		}
	}
	return -1
}

func (s *Store) renameTagInEntries(old, new string) {
	for _, e := range s.Entries {
		for i, t := range e.Tags {
			if strings.EqualFold(t, old) {
				e.Tags[i] = new
			}
		}
		e.Tags = NormalizeTags(e.Tags)
	}
}

func removeTag(tags []string, name string) []string {
	out := tags[:0]
	for _, t := range tags {
		if strings.EqualFold(t, name) {
			continue
		}
		out = append(out, t)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// NormalizeTagList cleans a tag list and gives every tag its table spelling.
func (c *Config) NormalizeTagList(tags []string) []string {
	clean := NormalizeTags(tags)
	for i, t := range clean {
		clean[i] = c.CanonicalTag(t)
	}
	return NormalizeTags(clean)
}

// Errors reported when a tool is incomplete.
var (
	ErrToolName    = errors.New("a tool needs a name")
	ErrToolCommand = errors.New("a tool needs a command")
)

// platformOpener is the launcher used on this platform when no tools are
// configured.
func platformOpener() string {
	switch runtime.GOOS {
	case "darwin":
		return "open"
	case "windows":
		return "cmd /c start"
	}
	return "xdg-open"
}

// ParseToolSpec parses "name=command[,tui|gui]", the form used by --tool.
//
//	zathura=zathura --fork,tui
//	firefox=firefox
//
// The suffix is only treated as a type when it is one of the two known words,
// so a command that itself contains a comma keeps working.
func ParseToolSpec(spec string) (Tool, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return Tool{}, ErrToolName
	}
	name, rest, ok := strings.Cut(spec, "=")
	name = strings.TrimSpace(name)
	command := strings.TrimSpace(rest)
	if !ok || name == "" || command == "" {
		return Tool{}, fmt.Errorf("%s: expected name=command[,tui|gui]", spec)
	}
	kind := ToolGUI
	if i := strings.LastIndexByte(command, ','); i >= 0 {
		suffix := strings.ToLower(strings.TrimSpace(command[i+1:]))
		if suffix == ToolGUI || suffix == ToolTUI {
			kind = suffix
			command = strings.TrimSpace(command[:i])
		}
	}
	if command == "" {
		return Tool{}, fmt.Errorf("%s: expected a command before the type", spec)
	}
	return Tool{Name: name, Command: command, Type: kind}, nil
}

// empty reports whether the config carries nothing worth writing.
func (c Config) empty() bool {
	return !c.UseEditor && !c.SearchMeta && len(c.Tools) == 0 &&
		c.DefaultTool == "" && len(c.TagTable) == 0 && c.LegacyOpener == ""
}

// ---- tag table integrity -------------------------------------------------

// TagProblem is one tag on a paper that the tag table does not know about, or
// whose name cannot be used.
type TagProblem struct {
	Entry string // file name
	Tag   string // the offending tag, spelled as the entry has it

	// Critical marks a name that breaks the filter parser, so a query cannot
	// refer to it without quoting: a space, "&&", "||" or a quote. A non-critical
	// problem (a comma, say) is worth reporting but does not get in the way, so
	// it is hidden from no display and refused at no point.
	Critical bool
}

// Problems lists every tag used by an entry that is missing from the tag table.
//
// This is reported rather than repaired on load: the store may have been edited
// by hand or by another tool, and quietly deleting a tag would destroy the
// user's intent (a typo like "kv-cache" is one corrected keystroke away from a
// real tag, but unrecoverable once deleted). The file is left byte-identical
// until the user decides what to do.
func (s *Store) Problems() []TagProblem {
	names := make([]string, 0, len(s.Entries))
	for name := range s.Entries {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []TagProblem
	for _, name := range names {
		e := s.Entries[name]
		if e == nil {
			continue
		}
		for _, t := range e.Tags {
			if !ValidTagName(t) {
				out = append(out, TagProblem{Entry: name, Tag: t, Critical: CriticalTagName(t)})
				continue
			}
			if _, ok := s.Config.Tag(t); !ok {
				out = append(out, TagProblem{Entry: name, Tag: t})
			}
		}
	}
	return out
}

// UnknownTags is the distinct set of unknown tag names, for a one-line warning.
// Matching is case-insensitive and the result is sorted.
func (s *Store) UnknownTags() []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range s.Problems() {
		key := strings.ToLower(p.Tag)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p.Tag)
	}
	sort.Strings(out)
	return out
}

// ProblemTagsFor is the set of tags on one entry that the table does not know,
// lowercased, so a display or a form can mark them.
func (s *Store) ProblemTagsFor(name string) map[string]bool {
	e := s.Entries[name]
	if e == nil {
		return nil
	}
	var out map[string]bool
	for _, t := range e.Tags {
		if !s.BlocksTag(t) {
			continue
		}
		if out == nil {
			out = map[string]bool{}
		}
		out[strings.ToLower(t)] = true
	}
	return out
}

// BlocksTag reports whether a tag cannot be used: the table does not know it, or
// its name would have to be quoted in every query. A known name with an untidy
// character is not blocked -- it is displayed and matched like any other.
func (s *Store) BlocksTag(t string) bool {
	if _, ok := s.Config.Tag(t); !ok {
		return true
	}
	return CriticalTagName(t)
}

// CriticalProblems lists only the problems that get in the way of querying or
// saving. Non-critical ones are reported by --check and otherwise left alone:
// a comma in a name is untidy, not dangerous.
func (s *Store) CriticalProblems() []TagProblem {
	var out []TagProblem
	for _, p := range s.Problems() {
		if p.Critical {
			out = append(out, p)
		}
	}
	return out
}

// UnknownTags reports the tags that must be resolved: those the table does not
// know, plus the ones whose name would break a query. A non-critical name that
// is otherwise known is not included, so an old tag with a comma keeps working.
func (s *Store) BlockingTags() []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range s.Problems() {
		if !s.BlocksTag(p.Tag) {
			continue
		}
		key := strings.ToLower(p.Tag)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, p.Tag)
	}
	sort.Strings(out)
	return out
}

// ValidTags returns only the tags of an entry that the table knows, in their
// table spelling. It is what a display uses, so an unknown tag is hidden rather
// than shown as if it were real.
func (s *Store) ValidTags(name string) []string {
	e := s.Entries[name]
	if e == nil {
		return nil
	}
	out := make([]string, 0, len(e.Tags))
	for _, t := range e.Tags {
		if !ValidTagName(t) {
			continue
		}
		if tag, ok := s.Config.Tag(t); ok {
			out = append(out, tag.Name)
		}
	}
	return NormalizeTags(out)
}

// canonicalizeTags rewrites an entry's tag list so that every tag uses the
// table's spelling and appears once.
//
// This is the one repair applied on load without asking, because it cannot lose
// information: the tag itself survives, only a duplicate of it (in another case)
// and a stray difference in spelling are dropped. Unknown tags are deliberately
// left alone -- see Problems.
func (s *Store) canonicalizeTags() {
	for _, e := range s.Entries {
		if e == nil || len(e.Tags) == 0 {
			continue
		}
		out := make([]string, 0, len(e.Tags))
		seen := make(map[string]bool, len(e.Tags))
		for _, t := range NormalizeTags(e.Tags) {
			if tag, ok := s.Config.Tag(t); ok {
				t = tag.Name
			}
			key := strings.ToLower(t)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, t)
		}
		e.Tags = out
	}
}

// canonicalizeTable drops duplicate tag rows (case-insensitively) from the tag
// table itself, keeping the first spelling and the first non-empty
// description. Duplicates can only get in by hand-editing the file.
func (c *Config) canonicalizeTable() {
	out := make([]Tag, 0, len(c.TagTable))
	seen := make(map[string]int, len(c.TagTable))
	for _, t := range c.TagTable {
		t.Name = strings.TrimSpace(t.Name)
		t.Description = strings.TrimSpace(t.Description)
		// Only an empty name is dropped. A name outside the current rule is kept
		// as it is: the rule governs what may be written, not what may exist, and
		// silently deleting the user's tag row would be the worst possible
		// reading of "report a problem".
		if t.Name == "" {
			continue
		}
		key := strings.ToLower(t.Name)
		if i, dup := seen[key]; dup {
			// Keep a description that the first row lacked.
			if out[i].Description == "" && t.Description != "" {
				out[i].Description = t.Description
			}
			continue
		}
		seen[key] = len(out)
		out = append(out, t)
	}
	c.TagTable = out
}

// Normalize repairs what can be repaired losslessly: duplicate tag rows and
// per-entry duplicates in a different case. It is called on load, so a file
// hand-edited into that state behaves as if it had been merged.
func (s *Store) Normalize() {
	s.Config.canonicalizeTable()
	s.canonicalizeTags()
}

// ValidateTags checks a tag list against the table, returning the names that
// cannot be written: the ones the table does not know, and the ones whose name
// would have to be quoted in every query.
//
// A known name with an untidy character is accepted. The rule on writing is a
// courtesy that keeps new data tidy; enforcing it on old data would hide the
// user's own tags from them over a punctuation mark.
func (s *Store) ValidateTags(tags []string) []string {
	var bad []string
	for _, t := range NormalizeTags(tags) {
		if _, ok := s.Config.Tag(t); !ok {
			bad = append(bad, t)
			continue
		}
		if CriticalTagName(t) {
			bad = append(bad, t)
		}
	}
	return bad
}
