package dbstore

import (
	"context"
	"database/sql"
	"sort"
	"time"

	"github.com/keegancsmith/sqlf"
	"github.com/opentracing/opentracing-go/log"

	"github.com/sourcegraph/sourcegraph/internal/database/basestore"
	"github.com/sourcegraph/sourcegraph/internal/observation"
)

type SourcedCommits struct {
	RepositoryID   int
	RepositoryName string
	Commits        []string
}

// ScanSourcedCommits scans triples of repository ids/repository names/commits from the
// return value of `*Store.query`. The output of this function is ordered by repository
// identifier, then by commit.
func ScanSourcedCommits(rows *sql.Rows, queryErr error) (_ []SourcedCommits, err error) {
	if queryErr != nil {
		return nil, queryErr
	}
	defer func() { err = basestore.CloseRows(rows, err) }()

	sourcedCommitsMap := map[int]SourcedCommits{}
	for rows.Next() {
		var repositoryID int
		var repositoryName string
		var commit string
		if err := rows.Scan(&repositoryID, &repositoryName, &commit); err != nil {
			return nil, err
		}

		sourcedCommitsMap[repositoryID] = SourcedCommits{
			RepositoryID:   repositoryID,
			RepositoryName: repositoryName,
			Commits:        append(sourcedCommitsMap[repositoryID].Commits, commit),
		}
	}

	flattened := make([]SourcedCommits, 0, len(sourcedCommitsMap))
	for _, sourcedCommits := range sourcedCommitsMap {
		sort.Strings(sourcedCommits.Commits)
		flattened = append(flattened, sourcedCommits)
	}

	sort.Slice(flattened, func(i, j int) bool {
		return flattened[i].RepositoryID < flattened[j].RepositoryID
	})
	return flattened, nil
}

// StaleSourcedCommits returns a set of commits attached to repositories that have been
// least recently checked for resolvability via gitserver. We do this periodically in
// order to determine which records in the database are unreachable by normal query
// paths and clean up that occupied (but useless) space. The output is of this method is
// ordered by repository ID then by commit.
func (s *Store) StaleSourcedCommits(ctx context.Context, minimumTimeSinceLastCheck time.Duration, limit int, now time.Time) (_ []SourcedCommits, err error) {
	ctx, traceLog, endObservation := s.operations.staleSourcedCommits.WithAndLogger(ctx, &err, observation.Args{})
	defer endObservation(1, observation.Args{})

	now = now.UTC()
	interval := int(minimumTimeSinceLastCheck / time.Second)
	uploadSubquery := sqlf.Sprintf(staleSourcedCommitsSubquery, sqlf.Sprintf("lsif_uploads"), now, interval)
	indexesSubquery := sqlf.Sprintf(staleSourcedCommitsSubquery, sqlf.Sprintf("lsif_indexes"), now, interval)

	sourcedCommits, err := ScanSourcedCommits(s.Store.Query(ctx, sqlf.Sprintf(staleSourcedCommitsQuery, uploadSubquery, indexesSubquery, limit)))
	if err != nil {
		return nil, err
	}

	numCommits := 0
	for _, commits := range sourcedCommits {
		numCommits += len(commits.Commits)
	}
	traceLog(
		log.Int("numRepositories", len(sourcedCommits)),
		log.Int("numCommits", numCommits),
	)

	return sourcedCommits, nil
}

const staleSourcedCommitsQuery = `
-- source: enterprise/internal/codeintel/stores/dbstore/janitor.go:StaleSourcedCommits
WITH
candidates AS (%s UNION %s)
SELECT r.id, r.name, c.commit
FROM candidates c
JOIN repo r ON r.id = c.repository_id
-- Order results so that the repositories with the commits that have been updated
-- the least frequently come first. Once a number of commits are processed from a
-- given repository the ordering may change.
ORDER BY MIN(c.max_last_checked_at) OVER (PARTITION BY c.repository_id), c.commit
LIMIT %s
`

const staleSourcedCommitsSubquery = `
SELECT
	repository_id,
	commit,
	-- Keep track of the most recent update of this commit that we know about
	-- as any earlier dates for the same repository and commit pair carry no
	-- useful information.
	MAX(commit_last_checked_at) as max_last_checked_at
FROM %s
WHERE
	-- Ignore records already marked as deleted
	state NOT IN ('deleted', 'deleting') AND
	-- Ignore records that have been checked recently. Note this condition is
	-- true for a null commit_last_checked_at (which has never been checked).
	(%s - commit_last_checked_at > (%s * '1 second'::interval)) IS DISTINCT FROM FALSE
GROUP BY repository_id, commit
`

// RefreshCommitResolvability will update each upload and index record belonging to the
// given repository identifier and commit. If the delete flag is true, then the state of
// each matching record will be soft-deleted. Regardless, the commit_last_checked_at value
// will be bumped to the current (given) time. This method returns the count of upload and
// index records modified, respectively.
func (s *Store) RefreshCommitResolvability(ctx context.Context, repositoryID int, commit string, delete bool, now time.Time) (uploadsUpdated int, indexesUpdated int, err error) {
	ctx, traceLog, endObservation := s.operations.refreshCommitResolvability.WithAndLogger(ctx, &err, observation.Args{LogFields: []log.Field{
		log.Int("repositoryID", repositoryID),
		log.String("commit", commit),
		log.Bool("delete", delete),
	}})
	defer endObservation(1, observation.Args{})

	var query *sqlf.Query
	if delete {
		query = sqlf.Sprintf(
			refreshCommitResolvabilityDeleteQuery,
			repositoryID, commit, // candidate_uploads
			repositoryID, commit, // candidate_indexes
			now, // delete_uploads
		)
	} else {
		query = sqlf.Sprintf(
			refreshCommitResolvabilityUpdateQuery,
			repositoryID, commit, // candidate_uploads
			repositoryID, commit, // candidate_indexes
			now, // update_uploads
			now, // update_indexes
		)
	}

	uploadsUpdated, indexesUpdated, err = scanPairOfCounts(s.Query(ctx, query))
	if err != nil {
		return 0, 0, err
	}
	traceLog(
		log.Int("uploadsUpdated", uploadsUpdated),
		log.Int("indexesUpdated", indexesUpdated),
	)

	return uploadsUpdated, indexesUpdated, nil
}

const refreshCommitResolvabilityUpdateQuery = `
-- source: enterprise/internal/codeintel/stores/dbstore/janitor.go:RefreshCommitResolvability
WITH
` + refreshCommitResolvabilityQueryCandidateUploadsCTE + `,
` + refreshCommitResolvabilityQueryCandidateIndexesCTE + `,
` + refreshCommitResolvabilityQueryUpdateUploadsCTE + `,
` + refreshCommitResolvabilityQueryUpdateIndexesCTE + `
SELECT
	(SELECT COUNT(*) FROM update_uploads) AS num_uploads,
	(SELECT COUNT(*) FROM update_indexes) AS num_indexes
`

const refreshCommitResolvabilityDeleteQuery = `
-- source: enterprise/internal/codeintel/stores/dbstore/janitor.go:RefreshCommitResolvability
WITH
` + refreshCommitResolvabilityQueryCandidateUploadsCTE + `,
` + refreshCommitResolvabilityQueryCandidateIndexesCTE + `,
` + refreshCommitResolvabilityQueryDeleteUploadsCTE + `,
` + refreshCommitResolvabilityQueryDeleteIndexesCTE + `
SELECT
	(SELECT COUNT(*) FROM delete_uploads) AS num_uploads,
	(SELECT COUNT(*) FROM delete_indexes) AS num_indexes
`

const refreshCommitResolvabilityQueryCandidateUploadsCTE = `
candidate_uploads AS (
	SELECT u.id
	FROM lsif_uploads u
	WHERE u.repository_id = %s AND u.commit = %s

	-- Lock these rows in a deterministic order so that we don't
	-- deadlock with other processes updating the lsif_uploads table.
	ORDER BY u.id FOR UPDATE
)
`

const refreshCommitResolvabilityQueryCandidateIndexesCTE = `
candidate_indexes AS (
	SELECT u.id
	FROM lsif_indexes u
	WHERE u.repository_id = %s AND u.commit = %s

	-- Lock these rows in a deterministic order so that we don't
	-- deadlock with other processes updating the lsif_indexes table.
	ORDER BY u.id FOR UPDATE
)
`

const refreshCommitResolvabilityQueryUpdateUploadsCTE = `
update_uploads AS (
	UPDATE lsif_uploads u
	SET commit_last_checked_at = %s
	WHERE id IN (SELECT id FROM candidate_uploads)
	RETURNING 1
)
`

const refreshCommitResolvabilityQueryUpdateIndexesCTE = `
update_indexes AS (
	UPDATE lsif_indexes u
	SET commit_last_checked_at = %s
	WHERE id IN (SELECT id FROM candidate_indexes)
	RETURNING 1
)
`

const refreshCommitResolvabilityQueryDeleteUploadsCTE = `
delete_uploads AS (
	UPDATE lsif_uploads u
	SET
		commit_last_checked_at = %s,
		state = CASE WHEN u.state = 'completed' THEN 'deleting' ELSE 'deleted' END
	WHERE id IN (SELECT id FROM candidate_uploads)
	RETURNING 1
)
`

const refreshCommitResolvabilityQueryDeleteIndexesCTE = `
delete_indexes AS (
	DELETE FROM lsif_indexes u
	WHERE id IN (SELECT id FROM candidate_indexes)
	RETURNING 1
)
`

func scanPairOfCounts(rows *sql.Rows, queryErr error) (value1, value2 int, err error) {
	if queryErr != nil {
		return 0, 0, queryErr
	}
	defer func() { err = basestore.CloseRows(rows, err) }()

	for rows.Next() {
		if err := rows.Scan(&value1, &value2); err != nil {
			return 0, 0, err
		}
	}

	return value1, value2, nil
}
