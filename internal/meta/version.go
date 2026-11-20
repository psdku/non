package meta

import (
	"errors"
	"fmt"
)

// The store carries a version so a schema change can be migrated instead of
// guessed at. Only the shape of the file is versioned, not its content: adding
// an entry, a tag or a tool is not a schema change.
//
// Rules for changing the format:
//
//   - A change that older versions can survive unchanged (a new optional key,
//     a new optional section) does NOT bump the version. Older readers keep the
//     unknown key verbatim thanks to the Fields maps, and nothing is lost.
//   - A change that alters the meaning or the shape of existing data bumps the
//     version and adds a migration step below.
//   - A store written by a newer version than this binary knows about is
//     refused, not silently upgraded or downgraded: rewriting a file whose
//     schema you do not understand is how data gets destroyed.
const (
	// Version is the schema version written by this build.
	Version = 1
)

// ErrStoreTooNew is returned when the file was written by a newer build.
var ErrStoreTooNew = errors.New("store was written by a newer version of non")

// migration upgrades one version of the document to the next.
//
// A migration works on the decoded store and returns a new version. It must be
// idempotent for the version it applies to, so an interrupted run that wrote
// the old version back can be retried.
type migration struct {
	from int
	to   int
	why  string
	run  func(*Store) error
}

// migrations is ordered by from version. Version 1 is the initial schema, so
// there is nothing to do yet; when the format changes, this is where the step
// is added, and a test for it belongs next to TestStoreMigrationChain.
var migrations = []migration{}

// migrate brings the store up to Version, applying every step in order.
func (s *Store) migrate() error {
	if s.Version > Version {
		return fmt.Errorf("%w: file is version %d, this binary understands %d",
			ErrStoreTooNew, s.Version, Version)
	}
	if s.Version <= 0 {
		// A document without a version predates versioning. It is the v1
		// shape, which is exactly how version 1 is identified retroactively.
		s.Version = 1
	}
	for _, m := range migrations {
		if s.Version != m.from {
			continue
		}
		if err := m.run(s); err != nil {
			return fmt.Errorf("migrating v%d to v%d (%s): %w", m.from, m.to, m.why, err)
		}
		s.Version = m.to
	}
	if s.Version != Version {
		return fmt.Errorf("store is version %d after migration, expected %d", s.Version, Version)
	}
	return nil
}

// NeedsMigration reports whether the file on disk would be rewritten to reach
// the current schema. It is exposed for --version and for tests; Save always
// migrates in memory, so a user never has to run a migration command.
func (s *Store) NeedsMigration() bool { return s.Version < Version }
