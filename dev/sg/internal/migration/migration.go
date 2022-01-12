package migration

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"

	"github.com/sourcegraph/sourcegraph/dev/sg/internal/db"
	"github.com/sourcegraph/sourcegraph/dev/sg/root"
)

const upMigrationFileTemplate = `-- +++
-- parent: %d
-- +++

BEGIN;

-- Perform migration here.
--
-- See README.md. Highlights:
--  * Make migrations idempotent (use IF EXISTS)
--  * Make migrations backwards-compatible (old readers/writers must continue to work)
--  * Wrap your changes in a transaction. Note that CREATE INDEX CONCURRENTLY is an exception
--    and cannot be performed in a transaction. For such migrations, ensure that only one
--    statement is defined per migration to prevent query transactions from starting implicitly.

COMMIT;
`

const downMigrationFileTemplate = `BEGIN;

-- Undo the changes made in the up migration

COMMIT;
`

// RunAdd creates a new up/down migration file pair for the given database and
// returns the names of the new files. If there was an error, the filesystem should remain
// unmodified.
func RunAdd(database db.Database, migrationName string) (up, down string, _ error) {
	baseDir, err := MigrationDirectoryForDatabase(database)
	if err != nil {
		return "", "", err
	}

	// TODO: We can probably convert to migrations and use getMaxMigrationID
	names, err := ReadFilenamesNamesInDirectory(baseDir)
	if err != nil {
		return "", "", err
	}

	lastMigrationIndex, ok := ParseLastMigrationIndex(names)
	if !ok {
		return "", "", errors.New("no previous migrations exist")
	}

	upPath, downPath, err := MakeMigrationFilenames(database, lastMigrationIndex+1, migrationName)
	if err != nil {
		return "", "", err
	}

	contents := map[string]string{
		upPath:   fmt.Sprintf(upMigrationFileTemplate, lastMigrationIndex),
		downPath: downMigrationFileTemplate,
	}

	if err := writeMigrationFiles(contents); err != nil {
		return "", "", err
	}

	return upPath, downPath, nil
}

// MigrationDirectoryForDatabase returns the directory where migration files are stored for the
// given database.
func MigrationDirectoryForDatabase(database db.Database) (string, error) {
	repoRoot, err := root.RepositoryRoot()
	if err != nil {
		return "", err
	}

	return filepath.Join(repoRoot, "migrations", database.Name), nil
}

// MakeMigrationFilenames makes a pair of (absolute) paths to migration files with the
// given migration index and name.
func MakeMigrationFilenames(database db.Database, migrationIndex int, migrationName string) (up string, down string, _ error) {
	baseDir, err := MigrationDirectoryForDatabase(database)
	if err != nil {
		return "", "", err
	}

	upPath := filepath.Join(baseDir, fmt.Sprintf("%d_%s.up.sql", migrationIndex, migrationName))
	downPath := filepath.Join(baseDir, fmt.Sprintf("%d_%s.down.sql", migrationIndex, migrationName))
	return upPath, downPath, nil
}

// ParseMigrationIndex parse a filename and returns the migration index if the filename
// looks like a migration. Each migration filename has the form {unique_id}_{name}.{dir}.sql.
// This function returns a false-valued flag on failure. Leading directories are stripped
// from the input, so a basename or a full path can be supplied.
func ParseMigrationIndex(name string) (int, bool) {
	index, err := strconv.Atoi(strings.Split(filepath.Base(name), "_")[0])
	if err != nil {
		return 0, false
	}

	return index, true
}

// ParseLastMigrationIndex parses a list of filenames and returns the highest migration
// index available.
func ParseLastMigrationIndex(names []string) (int, bool) {
	indices := make([]int, 0, len(names))
	for _, name := range names {
		if index, ok := ParseMigrationIndex(name); ok {
			indices = append(indices, index)
		}
	}
	sort.Ints(indices)

	if len(indices) == 0 {
		return 0, false
	}

	return indices[len(indices)-1], true
}

// writeMigrationFiles writes the contents of migrationFileTemplate to the given filepaths.
func writeMigrationFiles(contents map[string]string) (err error) {
	defer func() {
		if err != nil {
			for path := range contents {
				// undo any changes to the fs on error
				_ = os.Remove(path)
			}
		}
	}()

	for path, contents := range contents {
		if err := os.WriteFile(path, []byte(contents), os.FileMode(0644)); err != nil {
			return err
		}
	}

	return nil
}

// ReadFilenamesNamesInDirectory returns a list of names in the given directory.
func ReadFilenamesNamesInDirectory(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	return names, nil
}
