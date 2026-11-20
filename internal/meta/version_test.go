package meta

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreWritesCurrentVersion(t *testing.T) {
	dir := t.TempDir()
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Set("a.pdf", &Entry{Title: "A"})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, StoreFile))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Version != Version {
		t.Errorf("written version = %d, want %d", doc.Version, Version)
	}
}

func TestStoreWithoutVersionIsTreatedAsV1(t *testing.T) {
	// A document written before versioning existed is the v1 shape.
	dir := t.TempDir()
	raw := `{"entries":{"a.pdf":{"title":"A"}}}`
	if err := os.WriteFile(filepath.Join(dir, StoreFile), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Version != Version {
		t.Errorf("version = %d, want %d", s.Version, Version)
	}
	if e, ok := s.Lookup("a.pdf"); !ok || e.Title != "A" {
		t.Errorf("entry lost: %+v", e)
	}
}

func TestStoreRefusesNewerVersion(t *testing.T) {
	// The important safety property: a file from a newer build must not be
	// rewritten by an older one, because that would silently drop whatever
	// the new schema added.
	dir := t.TempDir()
	path := filepath.Join(dir, StoreFile)
	raw := `{"version":99,"entries":{"a.pdf":{"title":"A"}},"future_field":{"x":1}}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); !errors.Is(err, ErrStoreTooNew) {
		t.Fatalf("Load error = %v, want ErrStoreTooNew", err)
	}
	// The file must be untouched.
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != raw {
		t.Errorf("file was modified:\n%s", b)
	}
	// A hand-built store cannot overwrite it either.
	s, _ := Load(t.TempDir())
	s.Version = 99
	s.path = path
	if err := s.Save(); !errors.Is(err, ErrStoreTooNew) {
		t.Errorf("Save error = %v, want ErrStoreTooNew", err)
	}
}

func TestStoreMigrationChain(t *testing.T) {
	// The chain must land exactly on Version for every older version, and must
	// never skip a step. This test is what will fail when a migration is added
	// incorrectly.
	for v := 1; v <= Version; v++ {
		s := &Store{Version: v, Entries: map[string]*Entry{"a.pdf": {Title: "A"}}}
		if err := s.migrate(); err != nil {
			t.Fatalf("migrate from v%d: %v", v, err)
		}
		if s.Version != Version {
			t.Errorf("from v%d landed on v%d", v, s.Version)
		}
		if e, ok := s.Entries["a.pdf"]; !ok || e.Title != "A" {
			t.Errorf("from v%d: entries lost", v)
		}
	}
	// Migrations must be contiguous: each step starts where the previous ended.
	expect := 1
	for _, m := range migrations {
		if m.from != expect {
			t.Errorf("migration from v%d breaks the chain (expected v%d)", m.from, expect)
		}
		if m.to != m.from+1 {
			t.Errorf("migration v%d→v%d must advance one version", m.from, m.to)
		}
		expect = m.to
	}
	if expect != Version {
		t.Errorf("migrations end at v%d, Version is %d", expect, Version)
	}
}

func TestMigrationRunsOnLoad(t *testing.T) {
	// Once a real migration exists this proves the load path applies it. With
	// the current empty chain it pins the plumbing instead.
	dir := t.TempDir()
	s := New()
	s.Set("a.pdf", &Entry{Title: "A"})
	if err := s.SaveTo(dir); err != nil {
		t.Fatal(err)
	}
	back, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if back.NeedsMigration() {
		t.Error("a freshly written store should not need a migration")
	}
}

func TestConfigUnknownKeysSurviveVersioning(t *testing.T) {
	// The version gate must not interfere with the forwards compatible parts of
	// the format: unknown keys in the same version are still preserved.
	dir := t.TempDir()
	path := filepath.Join(dir, StoreFile)
	raw := `{"version":1,"entries":{},"future":{"a":1}}`
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "future") {
		t.Errorf("unknown top level key dropped:\n%s", b)
	}
}
