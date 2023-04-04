package store

import (
	"context"
	"sort"
	"strings"

	"github.com/keegancsmith/sqlf"
	"github.com/lib/pq"
	"github.com/opentracing/opentracing-go/log"
	"go.opentelemetry.io/otel/attribute"

	"github.com/sourcegraph/sourcegraph/enterprise/internal/codeintel/uploads/shared"
	"github.com/sourcegraph/sourcegraph/internal/database"
	"github.com/sourcegraph/sourcegraph/internal/database/basestore"
	"github.com/sourcegraph/sourcegraph/internal/observation"
	"github.com/sourcegraph/sourcegraph/lib/codeintel/precise"
	"github.com/sourcegraph/sourcegraph/lib/errors"
)

// GetUploads returns a list of uploads and the total count of records matching the given conditions.
func (s *store) GetUploads(ctx context.Context, opts shared.GetUploadsOptions) (uploads []shared.Upload, totalCount int, err error) {
	ctx, trace, endObservation := s.operations.getUploads.With(ctx, &err, observation.Args{LogFields: buildGetUploadsLogFields(opts)})
	defer endObservation(1, observation.Args{})

	tableExpr, conds, cte := buildGetConditionsAndCte(opts)
	authzConds, err := database.AuthzQueryConds(ctx, database.NewDBWith(s.logger, s.db))
	if err != nil {
		return nil, 0, err
	}
	conds = append(conds, authzConds)

	var orderExpression *sqlf.Query
	if opts.OldestFirst {
		orderExpression = sqlf.Sprintf("uploaded_at, id DESC")
	} else {
		orderExpression = sqlf.Sprintf("uploaded_at DESC, id")
	}

	tx, err := s.transact(ctx)
	if err != nil {
		return nil, 0, err
	}
	defer func() { err = tx.Done(err) }()

	query := sqlf.Sprintf(
		getUploadsSelectQuery,
		buildCTEPrefix(cte),
		tableExpr,
		sqlf.Join(conds, " AND "),
		orderExpression,
		opts.Limit,
		opts.Offset,
	)
	uploads, err = scanUploadComplete(tx.db.Query(ctx, query))
	if err != nil {
		return nil, 0, err
	}
	trace.AddEvent("TODO Domain Owner",
		attribute.Int("numUploads", len(uploads)))

	countQuery := sqlf.Sprintf(
		getUploadsCountQuery,
		buildCTEPrefix(cte),
		tableExpr,
		sqlf.Join(conds, " AND "),
	)
	totalCount, _, err = basestore.ScanFirstInt(tx.db.Query(ctx, countQuery))
	if err != nil {
		return nil, 0, err
	}
	trace.AddEvent("TODO Domain Owner",
		attribute.Int("totalCount", totalCount),
	)

	return uploads, totalCount, nil
}

const getUploadsSelectQuery = `
%s -- Dynamic CTE definitions for use in the WHERE clause
SELECT
	u.id,
	u.commit,
	u.root,
	EXISTS (` + visibleAtTipSubselectQuery + `) AS visible_at_tip,
	u.uploaded_at,
	u.state,
	u.failure_message,
	u.started_at,
	u.finished_at,
	u.process_after,
	u.num_resets,
	u.num_failures,
	u.repository_id,
	repo.name,
	u.indexer,
	u.indexer_version,
	u.num_parts,
	u.uploaded_parts,
	u.upload_size,
	u.associated_index_id,
	u.content_type,
	u.should_reindex,
	s.rank,
	u.uncompressed_size
FROM %s
LEFT JOIN (` + uploadRankQueryFragment + `) s
ON u.id = s.id
JOIN repo ON repo.id = u.repository_id
WHERE %s
ORDER BY %s
LIMIT %d OFFSET %d
`

const getUploadsCountQuery = `
%s -- Dynamic CTE definitions for use in the WHERE clause
SELECT COUNT(*) AS count
FROM %s
JOIN repo ON repo.id = u.repository_id
WHERE %s
`

// GetUploadByID returns an upload by its identifier and boolean flag indicating its existence.
func (s *store) GetUploadByID(ctx context.Context, id int) (_ shared.Upload, _ bool, err error) {
	ctx, _, endObservation := s.operations.getUploadByID.With(ctx, &err, observation.Args{LogFields: []log.Field{log.Int("id", id)}})
	defer endObservation(1, observation.Args{})

	authzConds, err := database.AuthzQueryConds(ctx, database.NewDBWith(s.logger, s.db))
	if err != nil {
		return shared.Upload{}, false, err
	}

	return scanFirstUpload(s.db.Query(ctx, sqlf.Sprintf(getUploadByIDQuery, id, authzConds)))
}

const getUploadByIDQuery = `
SELECT
	u.id,
	u.commit,
	u.root,
	EXISTS (` + visibleAtTipSubselectQuery + `) AS visible_at_tip,
	u.uploaded_at,
	u.state,
	u.failure_message,
	u.started_at,
	u.finished_at,
	u.process_after,
	u.num_resets,
	u.num_failures,
	u.repository_id,
	repo.name,
	u.indexer,
	u.indexer_version,
	u.num_parts,
	u.uploaded_parts,
	u.upload_size,
	u.associated_index_id,
	u.content_type,
	u.should_reindex,
	s.rank,
	u.uncompressed_size
FROM lsif_uploads u
LEFT JOIN (` + uploadRankQueryFragment + `) s
ON u.id = s.id
JOIN repo ON repo.id = u.repository_id
WHERE repo.deleted_at IS NULL AND u.state != 'deleted' AND u.id = %s AND %s
`

// GetDumpsByIDs returns a set of dumps by identifiers.
func (s *store) GetDumpsByIDs(ctx context.Context, ids []int) (_ []shared.Dump, err error) {
	ctx, trace, endObservation := s.operations.getDumpsByIDs.With(ctx, &err, observation.Args{LogFields: []log.Field{
		log.Int("numIDs", len(ids)),
		log.String("ids", intsToString(ids)),
	}})
	defer endObservation(1, observation.Args{})

	if len(ids) == 0 {
		return nil, nil
	}

	var idx []*sqlf.Query
	for _, id := range ids {
		idx = append(idx, sqlf.Sprintf("%s", id))
	}

	dumps, err := scanDumps(s.db.Query(ctx, sqlf.Sprintf(getDumpsByIDsQuery, sqlf.Join(idx, ", "))))
	if err != nil {
		return nil, err
	}
	trace.AddEvent("TODO Domain Owner", attribute.Int("numDumps", len(dumps)))

	return dumps, nil
}

const getDumpsByIDsQuery = `
SELECT
	u.id,
	u.commit,
	u.root,
	EXISTS (` + visibleAtTipSubselectQuery + `) AS visible_at_tip,
	u.uploaded_at,
	u.state,
	u.failure_message,
	u.started_at,
	u.finished_at,
	u.process_after,
	u.num_resets,
	u.num_failures,
	u.repository_id,
	u.repository_name,
	u.indexer,
	u.indexer_version,
	u.associated_index_id
FROM lsif_dumps_with_repository_name u WHERE u.id IN (%s)
`

func (s *store) getUploadsByIDs(ctx context.Context, allowDeleted bool, ids ...int) (_ []shared.Upload, err error) {
	ctx, _, endObservation := s.operations.getUploadsByIDs.With(ctx, &err, observation.Args{LogFields: []log.Field{
		log.String("ids", intsToString(ids)),
	}})
	defer endObservation(1, observation.Args{})

	if len(ids) == 0 {
		return nil, nil
	}

	authzConds, err := database.AuthzQueryConds(ctx, database.NewDBWith(s.logger, s.db))
	if err != nil {
		return nil, err
	}

	queries := make([]*sqlf.Query, 0, len(ids))
	for _, id := range ids {
		queries = append(queries, sqlf.Sprintf("%d", id))
	}

	cond := sqlf.Sprintf("TRUE")
	if !allowDeleted {
		cond = sqlf.Sprintf("u.state != 'deleted'")
	}

	return scanUploadComplete(s.db.Query(ctx, sqlf.Sprintf(getUploadsByIDsQuery, cond, sqlf.Join(queries, ", "), authzConds)))
}

// GetUploadsByIDs returns an upload for each of the given identifiers. Not all given ids will necessarily
// have a corresponding element in the returned list.
func (s *store) GetUploadsByIDs(ctx context.Context, ids ...int) (_ []shared.Upload, err error) {
	return s.getUploadsByIDs(ctx, false, ids...)
}

func (s *store) GetUploadsByIDsAllowDeleted(ctx context.Context, ids ...int) (_ []shared.Upload, err error) {
	return s.getUploadsByIDs(ctx, true, ids...)
}

const getUploadsByIDsQuery = `
SELECT
	u.id,
	u.commit,
	u.root,
	EXISTS (` + visibleAtTipSubselectQuery + `) AS visible_at_tip,
	u.uploaded_at,
	u.state,
	u.failure_message,
	u.started_at,
	u.finished_at,
	u.process_after,
	u.num_resets,
	u.num_failures,
	u.repository_id,
	repo.name,
	u.indexer,
	u.indexer_version,
	u.num_parts,
	u.uploaded_parts,
	u.upload_size,
	u.associated_index_id,
	u.content_type,
	u.should_reindex,
	s.rank,
	u.uncompressed_size
FROM lsif_uploads u
LEFT JOIN (` + uploadRankQueryFragment + `) s
ON u.id = s.id
JOIN repo ON repo.id = u.repository_id
WHERE repo.deleted_at IS NULL AND %s AND u.id IN (%s) AND %s
`

// GetUploadIDsWithReferences returns uploads that probably contain an import
// or implementation moniker whose identifier matches any of the given monikers' identifiers. This method
// will not return uploads for commits which are unknown to gitserver, nor will it return uploads which
// are listed in the given ignored identifier slice. This method also returns the number of records
// scanned (but possibly filtered out from the return slice) from the database (the offset for the
// subsequent request) and the total number of records in the database.
func (s *store) GetUploadIDsWithReferences(
	ctx context.Context,
	orderedMonikers []precise.QualifiedMonikerData,
	ignoreIDs []int,
	repositoryID int,
	commit string,
	limit int,
	offset int,
	trace observation.TraceLogger,
) (ids []int, recordsScanned int, totalCount int, err error) {
	scanner, totalCount, err := s.GetVisibleUploadsMatchingMonikers(ctx, repositoryID, commit, orderedMonikers, limit, offset)
	if err != nil {
		return nil, 0, 0, errors.Wrap(err, "dbstore.ReferenceIDs")
	}

	defer func() {
		if closeErr := scanner.Close(); closeErr != nil {
			err = errors.Append(err, errors.Wrap(closeErr, "dbstore.ReferenceIDs.Close"))
		}
	}()

	ignoreIDsMap := map[int]struct{}{}
	for _, id := range ignoreIDs {
		ignoreIDsMap[id] = struct{}{}
	}

	filtered := map[int]struct{}{}

	for len(filtered) < limit {
		packageReference, exists, err := scanner.Next()
		if err != nil {
			return nil, 0, 0, errors.Wrap(err, "dbstore.GetUploadIDsWithReferences.Next")
		}
		if !exists {
			break
		}
		recordsScanned++

		if _, ok := filtered[packageReference.DumpID]; ok {
			// This index includes a definition so we can skip testing the filters here. The index
			// will be included in the moniker search regardless if it contains additional references.
			continue
		}

		if _, ok := ignoreIDsMap[packageReference.DumpID]; ok {
			// Ignore this dump
			continue
		}

		filtered[packageReference.DumpID] = struct{}{}
	}

	if trace != nil {
		trace.AddEvent("TODO Domain Owner",
			attribute.Int("uploadIDsWithReferences.numFiltered", len(filtered)),
			attribute.Int("uploadIDsWithReferences.numRecordsScanned", recordsScanned))
	}

	flattened := make([]int, 0, len(filtered))
	for k := range filtered {
		flattened = append(flattened, k)
	}
	sort.Ints(flattened)

	return flattened, recordsScanned, totalCount, nil
}

// GetVisibleUploadsMatchingMonikers returns visible uploads that refer (via package information) to any of the
// given monikers' packages.
//
// Visibility is determined in two parts: if the index belongs to the given repository, it is visible if
// it can be seen from the given index; otherwise, an index is visible if it can be seen from the tip of
// the default branch of its own repository.
// ReferenceIDs
func (s *store) GetVisibleUploadsMatchingMonikers(ctx context.Context, repositoryID int, commit string, monikers []precise.QualifiedMonikerData, limit, offset int) (_ shared.PackageReferenceScanner, _ int, err error) {
	ctx, trace, endObservation := s.operations.getVisibleUploadsMatchingMonikers.With(ctx, &err, observation.Args{LogFields: []log.Field{
		log.Int("repositoryID", repositoryID),
		log.String("commit", commit),
		log.Int("numMonikers", len(monikers)),
		log.String("monikers", monikersToString(monikers)),
		log.Int("limit", limit),
		log.Int("offset", offset),
	}})
	defer endObservation(1, observation.Args{})

	if len(monikers) == 0 {
		return PackageReferenceScannerFromSlice(), 0, nil
	}

	qs := make([]*sqlf.Query, 0, len(monikers))
	for _, moniker := range monikers {
		qs = append(qs, sqlf.Sprintf("(%s, %s, %s, %s)", moniker.Scheme, moniker.Manager, moniker.Name, moniker.Version))
	}

	visibleUploadsQuery := makeVisibleUploadsQuery(repositoryID, commit)

	authzConds, err := database.AuthzQueryConds(ctx, database.NewDBWith(s.logger, s.db))
	if err != nil {
		return nil, 0, err
	}

	countQuery := sqlf.Sprintf(referenceIDsCountQuery, visibleUploadsQuery, repositoryID, sqlf.Join(qs, ", "), authzConds)
	totalCount, _, err := basestore.ScanFirstInt(s.db.Query(ctx, countQuery))
	if err != nil {
		return nil, 0, err
	}
	trace.AddEvent("TODO Domain Owner", attribute.Int("totalCount", totalCount))

	query := sqlf.Sprintf(referenceIDsQuery, visibleUploadsQuery, repositoryID, sqlf.Join(qs, ", "), authzConds, limit, offset)
	rows, err := s.db.Query(ctx, query)
	if err != nil {
		return nil, 0, err
	}

	return PackageReferenceScannerFromRows(rows), totalCount, nil
}

// GetDumpsWithDefinitionsForMonikers returns the set of dumps that define at least one of the given monikers.
func (s *store) GetDumpsWithDefinitionsForMonikers(ctx context.Context, monikers []precise.QualifiedMonikerData) (_ []shared.Dump, err error) {
	ctx, trace, endObservation := s.operations.getDumpsWithDefinitionsForMonikers.With(ctx, &err, observation.Args{LogFields: []log.Field{
		log.Int("numMonikers", len(monikers)),
		log.String("monikers", monikersToString(monikers)),
	}})
	defer endObservation(1, observation.Args{})

	if len(monikers) == 0 {
		return nil, nil
	}

	qs := make([]*sqlf.Query, 0, len(monikers))
	for _, moniker := range monikers {
		qs = append(qs, sqlf.Sprintf("(%s, %s, %s, %s)", moniker.Scheme, moniker.Manager, moniker.Name, moniker.Version))
	}

	authzConds, err := database.AuthzQueryConds(ctx, database.NewDBWith(s.logger, s.db))
	if err != nil {
		return nil, err
	}

	query := sqlf.Sprintf(definitionDumpsQuery, sqlf.Join(qs, ", "), authzConds, DefinitionDumpsLimit)
	dumps, err := scanDumps(s.db.Query(ctx, query))
	if err != nil {
		return nil, err
	}
	trace.AddEvent("TODO Domain Owner", attribute.Int("numDumps", len(dumps)))

	return dumps, nil
}

const definitionDumpsQuery = `
WITH
ranked_uploads AS (
	SELECT
		u.id,
		-- Rank each upload providing the same package from the same directory
		-- within a repository by commit date. We'll choose the oldest commit
		-- date as the canonical choice used to resolve the current definitions
		-- request.
		` + packageRankingQueryFragment + ` AS rank
	FROM lsif_uploads u
	JOIN lsif_packages p ON p.dump_id = u.id
	JOIN repo ON repo.id = u.repository_id
	WHERE
		-- Don't match deleted uploads
		u.state = 'completed' AND
		(p.scheme, p.manager, p.name, p.version) IN (%s) AND
		%s -- authz conds
),
canonical_uploads AS (
	SELECT ru.id
	FROM ranked_uploads ru
	WHERE ru.rank = 1
	ORDER BY ru.id
	LIMIT %s
)
SELECT
	u.id,
	u.commit,
	u.root,
	EXISTS (` + visibleAtTipSubselectQuery + `) AS visible_at_tip,
	u.uploaded_at,
	u.state,
	u.failure_message,
	u.started_at,
	u.finished_at,
	u.process_after,
	u.num_resets,
	u.num_failures,
	u.repository_id,
	u.repository_name,
	u.indexer,
	u.indexer_version,
	u.associated_index_id
FROM lsif_dumps_with_repository_name u
WHERE u.id IN (SELECT id FROM canonical_uploads)
`

// GetAuditLogsForUpload returns all the audit logs for the given upload ID in order of entry
// from oldest to newest, according to the auto-incremented internal sequence field.
func (s *store) GetAuditLogsForUpload(ctx context.Context, uploadID int) (_ []shared.UploadLog, err error) {
	authzConds, err := database.AuthzQueryConds(ctx, database.NewDBWith(s.logger, s.db))
	if err != nil {
		return nil, err
	}

	return scanUploadAuditLogs(s.db.Query(ctx, sqlf.Sprintf(getAuditLogsForUploadQuery, uploadID, authzConds)))
}

const getAuditLogsForUploadQuery = `
SELECT
	u.log_timestamp,
	u.record_deleted_at,
	u.upload_id,
	u.commit,
	u.root,
	u.repository_id,
	u.uploaded_at,
	u.indexer,
	u.indexer_version,
	u.upload_size,
	u.associated_index_id,
	u.transition_columns,
	u.reason,
	u.operation
FROM lsif_uploads_audit_logs u
JOIN repo ON repo.id = u.repository_id
WHERE u.upload_id = %s AND %s
ORDER BY u.sequence
`

// DeleteUploads deletes uploads by filter criteria. The associated repositories will be marked as dirty
// so that their commit graphs will be updated in the background.
func (s *store) DeleteUploads(ctx context.Context, opts shared.DeleteUploadsOptions) (err error) {
	ctx, _, endObservation := s.operations.deleteUploads.With(ctx, &err, observation.Args{LogFields: buildDeleteUploadsLogFields(opts)})
	defer endObservation(1, observation.Args{})

	conds := buildDeleteConditions(opts)
	authzConds, err := database.AuthzQueryConds(ctx, database.NewDBWith(s.logger, s.db))
	if err != nil {
		return err
	}
	conds = append(conds, authzConds)

	tx, err := s.transact(ctx)
	if err != nil {
		return err
	}
	defer func() { err = tx.Done(err) }()

	unset, _ := tx.db.SetLocal(ctx, "codeintel.lsif_uploads_audit.reason", "direct delete by filter criteria request")
	defer unset(ctx)

	query := sqlf.Sprintf(
		deleteUploadsQuery,
		sqlf.Join(conds, " AND "),
	)
	repoIDs, err := basestore.ScanInts(s.db.Query(ctx, query))
	if err != nil {
		return err
	}

	var dirtyErr error
	for _, repoID := range repoIDs {
		if err := tx.SetRepositoryAsDirty(ctx, repoID); err != nil {
			dirtyErr = err
		}
	}
	if dirtyErr != nil {
		err = dirtyErr
	}

	return err
}

const deleteUploadsQuery = `
UPDATE lsif_uploads u
SET state = CASE WHEN u.state = 'completed' THEN 'deleting' ELSE 'deleted' END
FROM repo
WHERE repo.id = u.repository_id AND %s
RETURNING repository_id
`

// DeleteUploadByID deletes an upload by its identifier. This method returns a true-valued flag if a record
// was deleted. The associated repository will be marked as dirty so that its commit graph will be updated in
// the background.
func (s *store) DeleteUploadByID(ctx context.Context, id int) (_ bool, err error) {
	ctx, _, endObservation := s.operations.deleteUploadByID.With(ctx, &err, observation.Args{LogFields: []log.Field{log.Int("id", id)}})
	defer endObservation(1, observation.Args{})

	tx, err := s.transact(ctx)
	if err != nil {
		return false, err
	}
	defer func() { err = tx.Done(err) }()

	unset, _ := tx.db.SetLocal(ctx, "codeintel.lsif_uploads_audit.reason", "direct delete by ID request")
	defer unset(ctx)

	repositoryID, deleted, err := basestore.ScanFirstInt(tx.db.Query(ctx, sqlf.Sprintf(deleteUploadByIDQuery, id)))
	if err != nil {
		return false, err
	}
	if !deleted {
		return false, nil
	}

	if err := tx.SetRepositoryAsDirty(ctx, repositoryID); err != nil {
		return false, err
	}

	return true, nil
}

const deleteUploadByIDQuery = `
UPDATE lsif_uploads u SET state = CASE WHEN u.state = 'completed' THEN 'deleting' ELSE 'deleted' END WHERE id = %s RETURNING repository_id
`

// ReindexUploads reindexes uploads matching the given filter criteria.
func (s *store) ReindexUploads(ctx context.Context, opts shared.ReindexUploadsOptions) (err error) {
	ctx, _, endObservation := s.operations.reindexUploads.With(ctx, &err, observation.Args{LogFields: []log.Field{
		log.Int("repositoryID", opts.RepositoryID),
		log.String("states", strings.Join(opts.States, ",")),
		log.String("term", opts.Term),
		log.Bool("visibleAtTip", opts.VisibleAtTip),
	}})
	defer endObservation(1, observation.Args{})

	var conds []*sqlf.Query

	if opts.RepositoryID != 0 {
		conds = append(conds, sqlf.Sprintf("u.repository_id = %s", opts.RepositoryID))
	}
	if opts.Term != "" {
		conds = append(conds, makeSearchCondition(opts.Term))
	}
	if len(opts.States) > 0 {
		conds = append(conds, makeStateCondition(opts.States))
	}
	if opts.VisibleAtTip {
		conds = append(conds, sqlf.Sprintf("EXISTS ("+visibleAtTipSubselectQuery+")"))
	}
	if len(opts.IndexerNames) != 0 {
		var indexerConds []*sqlf.Query
		for _, indexerName := range opts.IndexerNames {
			indexerConds = append(indexerConds, sqlf.Sprintf("u.indexer ILIKE %s", "%"+indexerName+"%"))
		}

		conds = append(conds, sqlf.Sprintf("(%s)", sqlf.Join(indexerConds, " OR ")))
	}

	authzConds, err := database.AuthzQueryConds(ctx, database.NewDBWith(s.logger, s.db))
	if err != nil {
		return err
	}
	conds = append(conds, authzConds)

	tx, err := s.transact(ctx)
	if err != nil {
		return err
	}
	defer func() { err = tx.db.Done(err) }()

	unset, _ := tx.db.SetLocal(ctx, "codeintel.lsif_uploads_audit.reason", "direct reindex by filter criteria request")
	defer unset(ctx)

	err = tx.db.Exec(ctx, sqlf.Sprintf(reindexUploadsQuery, sqlf.Join(conds, " AND ")))
	if err != nil {
		return err
	}

	return nil
}

const reindexUploadsQuery = `
WITH
upload_candidates AS (
    SELECT u.id, u.associated_index_id
	FROM lsif_uploads u
	JOIN repo ON repo.id = u.repository_id
	WHERE %s
    ORDER BY u.id
    FOR UPDATE
),
update_uploads AS (
	UPDATE lsif_uploads u
	SET should_reindex = true
	WHERE u.id IN (SELECT id FROM upload_candidates)
),
index_candidates AS (
	SELECT u.id
	FROM lsif_indexes u
	WHERE u.id IN (SELECT associated_index_id FROM upload_candidates)
	ORDER BY u.id
	FOR UPDATE
)
UPDATE lsif_indexes u
SET should_reindex = true
WHERE u.id IN (SELECT id FROM index_candidates)
`

// ReindexUploadByID reindexes an upload by its identifier.
func (s *store) ReindexUploadByID(ctx context.Context, id int) (err error) {
	ctx, _, endObservation := s.operations.reindexUploadByID.With(ctx, &err, observation.Args{LogFields: []log.Field{
		log.Int("id", id),
	}})
	defer endObservation(1, observation.Args{})

	tx, err := s.transact(ctx)
	if err != nil {
		return err
	}
	defer func() { err = tx.db.Done(err) }()

	return tx.db.Exec(ctx, sqlf.Sprintf(reindexUploadByIDQuery, id, id))
}

const reindexUploadByIDQuery = `
WITH
update_uploads AS (
	UPDATE lsif_uploads u
	SET should_reindex = true
	WHERE id = %s
)
UPDATE lsif_indexes u
SET should_reindex = true
WHERE id IN (SELECT associated_index_id FROM lsif_uploads WHERE id = %s)
`

//
//

// makeStateCondition returns a disjunction of clauses comparing the upload against the target state.
func makeStateCondition(states []string) *sqlf.Query {
	stateMap := make(map[string]struct{}, 2)
	for _, state := range states {
		// Treat errored and failed states as equivalent
		if state == "errored" || state == "failed" {
			stateMap["errored"] = struct{}{}
			stateMap["failed"] = struct{}{}
		} else {
			stateMap[state] = struct{}{}
		}
	}

	orderedStates := make([]string, 0, len(stateMap))
	for state := range stateMap {
		orderedStates = append(orderedStates, state)
	}
	sort.Strings(orderedStates)

	if len(orderedStates) == 1 {
		return sqlf.Sprintf("u.state = %s", orderedStates[0])
	}

	return sqlf.Sprintf("u.state = ANY(%s)", pq.Array(orderedStates))
}

// makeSearchCondition returns a disjunction of LIKE clauses against all searchable columns of an upload.
func makeSearchCondition(term string) *sqlf.Query {
	searchableColumns := []string{
		"u.commit",
		"u.root",
		"(u.state)::text",
		"u.failure_message",
		"repo.name",
		"u.indexer",
		"u.indexer_version",
	}

	var termConds []*sqlf.Query
	for _, column := range searchableColumns {
		termConds = append(termConds, sqlf.Sprintf(column+" ILIKE %s", "%"+term+"%"))
	}

	return sqlf.Sprintf("(%s)", sqlf.Join(termConds, " OR "))
}

func buildDeleteConditions(opts shared.DeleteUploadsOptions) []*sqlf.Query {
	conds := []*sqlf.Query{}
	if opts.RepositoryID != 0 {
		conds = append(conds, sqlf.Sprintf("u.repository_id = %s", opts.RepositoryID))
	}
	conds = append(conds, sqlf.Sprintf("repo.deleted_at IS NULL"))
	conds = append(conds, sqlf.Sprintf("u.state != 'deleted'"))
	if opts.Term != "" {
		conds = append(conds, makeSearchCondition(opts.Term))
	}
	if len(opts.States) > 0 {
		conds = append(conds, makeStateCondition(opts.States))
	}
	if opts.VisibleAtTip {
		conds = append(conds, sqlf.Sprintf("EXISTS ("+visibleAtTipSubselectQuery+")"))
	}
	if len(opts.IndexerNames) != 0 {
		var indexerConds []*sqlf.Query
		for _, indexerName := range opts.IndexerNames {
			indexerConds = append(indexerConds, sqlf.Sprintf("u.indexer ILIKE %s", "%"+indexerName+"%"))
		}

		conds = append(conds, sqlf.Sprintf("(%s)", sqlf.Join(indexerConds, " OR ")))
	}

	return conds
}

func buildGetConditionsAndCte(opts shared.GetUploadsOptions) (*sqlf.Query, []*sqlf.Query, []cteDefinition) {
	conds := make([]*sqlf.Query, 0, 13)

	allowDeletedUploads := opts.AllowDeletedUpload && (opts.State == "" || opts.State == "deleted")

	if opts.RepositoryID != 0 {
		conds = append(conds, sqlf.Sprintf("u.repository_id = %s", opts.RepositoryID))
	}
	if opts.Term != "" {
		conds = append(conds, makeSearchCondition(opts.Term))
	}
	if opts.State != "" {
		opts.States = append(opts.States, opts.State)
	}
	if len(opts.States) > 0 {
		conds = append(conds, makeStateCondition(opts.States))
	} else if !allowDeletedUploads {
		conds = append(conds, sqlf.Sprintf("u.state != 'deleted'"))
	}
	if opts.VisibleAtTip {
		conds = append(conds, sqlf.Sprintf("EXISTS ("+visibleAtTipSubselectQuery+")"))
	}

	cteDefinitions := make([]cteDefinition, 0, 2)
	if opts.DependencyOf != 0 {
		cteDefinitions = append(cteDefinitions, cteDefinition{
			name:       "ranked_dependencies",
			definition: sqlf.Sprintf(rankedDependencyCandidateCTEQuery, sqlf.Sprintf("r.dump_id = %s", opts.DependencyOf)),
		})

		// Limit results to the set of uploads canonically providing packages referenced by the given upload identifier
		// (opts.DependencyOf). We do this by selecting the top ranked values in the CTE defined above, which are the
		// referenced package providers grouped by package name, version, repository, and root.
		conds = append(conds, sqlf.Sprintf(`u.id IN (SELECT rd.pkg_id FROM ranked_dependencies rd WHERE rd.rank = 1)`))
	}
	if opts.DependentOf != 0 {
		cteCondition := sqlf.Sprintf(`(p.scheme, p.manager, p.name, p.version) IN (
			SELECT p.scheme, p.manager, p.name, p.version
			FROM lsif_packages p
			WHERE p.dump_id = %s
		)`, opts.DependentOf)

		cteDefinitions = append(cteDefinitions, cteDefinition{
			name:       "ranked_dependents",
			definition: sqlf.Sprintf(rankedDependentCandidateCTEQuery, cteCondition),
		})

		// Limit results to the set of uploads that reference the target upload if it canonically provides the
		// matching package. If the target upload does not canonically provide a package, the results will contain
		// no dependent uploads.
		conds = append(conds, sqlf.Sprintf(`u.id IN (
			SELECT r.dump_id
			FROM ranked_dependents rd
			JOIN lsif_references r ON
				r.scheme = rd.scheme AND
				r.manager = rd.manager AND
				r.name = rd.name AND
				r.version = rd.version AND
				r.dump_id != rd.pkg_id
			WHERE rd.pkg_id = %s AND rd.rank = 1
		)`, opts.DependentOf))
	}

	if len(opts.IndexerNames) != 0 {
		var indexerConds []*sqlf.Query
		for _, indexerName := range opts.IndexerNames {
			indexerConds = append(indexerConds, sqlf.Sprintf("u.indexer ILIKE %s", "%"+indexerName+"%"))
		}

		conds = append(conds, sqlf.Sprintf("(%s)", sqlf.Join(indexerConds, " OR ")))
	}

	sourceTableExpr := sqlf.Sprintf("lsif_uploads u")
	if allowDeletedUploads {
		cteDefinitions = append(cteDefinitions, cteDefinition{
			name:       "deleted_uploads",
			definition: sqlf.Sprintf(deletedUploadsFromAuditLogsCTEQuery),
		})

		sourceTableExpr = sqlf.Sprintf(`(
			SELECT
				id,
				commit,
				root,
				uploaded_at,
				state,
				failure_message,
				started_at,
				finished_at,
				process_after,
				num_resets,
				num_failures,
				repository_id,
				indexer,
				indexer_version,
				num_parts,
				uploaded_parts,
				upload_size,
				associated_index_id,
				content_type,
				should_reindex,
				expired,
				uncompressed_size
			FROM lsif_uploads
			UNION ALL
			SELECT *
			FROM deleted_uploads
		) AS u`)
	}

	if opts.UploadedBefore != nil {
		conds = append(conds, sqlf.Sprintf("u.uploaded_at < %s", *opts.UploadedBefore))
	}
	if opts.UploadedAfter != nil {
		conds = append(conds, sqlf.Sprintf("u.uploaded_at > %s", *opts.UploadedAfter))
	}
	if opts.InCommitGraph {
		conds = append(conds, sqlf.Sprintf("u.finished_at < (SELECT updated_at FROM lsif_dirty_repositories ldr WHERE ldr.repository_id = u.repository_id)"))
	}
	if opts.LastRetentionScanBefore != nil {
		conds = append(conds, sqlf.Sprintf("(u.last_retention_scan_at IS NULL OR u.last_retention_scan_at < %s)", *opts.LastRetentionScanBefore))
	}
	if !opts.AllowExpired {
		conds = append(conds, sqlf.Sprintf("NOT u.expired"))
	}
	if !opts.AllowDeletedRepo {
		conds = append(conds, sqlf.Sprintf("repo.deleted_at IS NULL"))
	}
	// Never show uploads for deleted repos
	conds = append(conds, sqlf.Sprintf("repo.blocked IS NULL"))

	return sourceTableExpr, conds, cteDefinitions
}

func buildGetUploadsLogFields(opts shared.GetUploadsOptions) []log.Field {
	return []log.Field{
		log.Int("repositoryID", opts.RepositoryID),
		log.String("state", opts.State),
		log.String("term", opts.Term),
		log.Bool("visibleAtTip", opts.VisibleAtTip),
		log.Int("dependencyOf", opts.DependencyOf),
		log.Int("dependentOf", opts.DependentOf),
		log.String("uploadedBefore", nilTimeToString(opts.UploadedBefore)),
		log.String("uploadedAfter", nilTimeToString(opts.UploadedAfter)),
		log.String("lastRetentionScanBefore", nilTimeToString(opts.LastRetentionScanBefore)),
		log.Bool("inCommitGraph", opts.InCommitGraph),
		log.Bool("allowExpired", opts.AllowExpired),
		log.Bool("oldestFirst", opts.OldestFirst),
		log.Int("limit", opts.Limit),
		log.Int("offset", opts.Offset),
	}
}

func buildDeleteUploadsLogFields(opts shared.DeleteUploadsOptions) []log.Field {
	return []log.Field{
		log.String("states", strings.Join(opts.States, ",")),
		log.String("term", opts.Term),
		log.Bool("visibleAtTip", opts.VisibleAtTip),
	}
}

func buildCTEPrefix(cteDefinitions []cteDefinition) *sqlf.Query {
	if len(cteDefinitions) == 0 {
		return sqlf.Sprintf("")
	}

	cteQueries := make([]*sqlf.Query, 0, len(cteDefinitions))
	for _, cte := range cteDefinitions {
		cteQueries = append(cteQueries, sqlf.Sprintf("%s AS (%s)", sqlf.Sprintf(cte.name), cte.definition))
	}

	return sqlf.Sprintf("WITH\n%s", sqlf.Join(cteQueries, ",\n"))
}
