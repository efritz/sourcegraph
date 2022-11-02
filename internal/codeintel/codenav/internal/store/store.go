package store

import (
	"context"

	"github.com/keegancsmith/sqlf"
	"github.com/lib/pq"
	logger "github.com/sourcegraph/log"

	"github.com/sourcegraph/sourcegraph/internal/database"
	"github.com/sourcegraph/sourcegraph/internal/database/basestore"
	"github.com/sourcegraph/sourcegraph/internal/database/dbutil"
	"github.com/sourcegraph/sourcegraph/internal/observation"
)

// Store provides the interface for codenav storage.
type Store interface {
	GetUnsafeDB() database.DB
	GetUploadsForRanking(ctx context.Context, key string, batchSize int) ([]Upload, error)

	ProcessStaleExportedUplods(
		ctx context.Context,
		graphKey string,
		batchSize int,
		deleter func(ctx context.Context, id int) error,
	) (totalDeleted int, err error)
}

// store manages the codenav store.
type store struct {
	db         *basestore.Store
	logger     logger.Logger
	operations *operations
}

// New returns a new codenav store.
func New(db database.DB, observationContext *observation.Context) Store {
	return &store{
		db:         basestore.NewWithHandle(db.Handle()),
		logger:     logger.Scoped("codenav.store", ""),
		operations: newOperations(observationContext),
	}
}

// GetUnsafeDB returns the underlying database handle. This is used by the
// resolvers that have the old convention of using the database handle directly.
func (s *store) GetUnsafeDB() database.DB {
	return database.NewDBWith(s.logger, s.db)
}

type Upload struct {
	ID   int
	Repo string
	Root string
}

var scanUploads = basestore.NewSliceScanner(func(s dbutil.Scanner) (u Upload, _ error) {
	err := s.Scan(&u.ID, &u.Repo, &u.Root)
	return u, err
})

func (s *store) GetUploadsForRanking(ctx context.Context, key string, batchSize int) (_ []Upload, err error) {
	return scanUploads(s.db.Query(ctx, sqlf.Sprintf(
		getUploadsForRankingQuery,
		key,
		batchSize,
		key,
	)))
}

const getUploadsForRankingQuery = `
WITH candidates AS (
	SELECT u.id
	FROM lsif_uploads u
	JOIN repo r ON r.id = u.repository_id
	WHERE
		u.id IN (
			SELECT uvt.upload_id
			FROM lsif_uploads_visible_at_tip uvt
			WHERE uvt.is_default_branch
		) AND
		u.id NOT IN (
			SELECT re.upload_id
			FROM codeintel_ranking_exports re
			WHERE re.graph_key = %s
		) AND
		r.deleted_at IS NULL AND
		r.blocked IS NULL
	ORDER BY u.id DESC
	LIMIT %s
	FOR UPDATE SKIP LOCKED
),
inserted AS (
	INSERT INTO codeintel_ranking_exports (upload_id, graph_key)
	SELECT id, %s AS graph_key FROM candidates
	ON CONFLICT (upload_id, graph_key) DO NOTHING
	RETURNING upload_id AS id
)
SELECT
	u.id,
	r.name,
	u.root
FROM lsif_uploads u
JOIN repo r ON r.id = u.repository_id
WHERE u.id IN (SELECT id FROM inserted)
`

func (s *store) ProcessStaleExportedUplods(
	ctx context.Context,
	graphKey string,
	batchSize int,
	deleter func(ctx context.Context, id int) error,
) (totalDeleted int, err error) {
	tx, err := s.db.Transact(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { err = tx.Done(err) }()

	ids, err := basestore.ScanInts(tx.Query(ctx, sqlf.Sprintf(selectStaleExportedUploadsQuery, graphKey, batchSize)))
	if err != nil {
		return 0, err
	}

	for _, id := range ids {
		if err := deleter(ctx, id); err != nil {
			return 0, err
		}
	}

	if err := tx.Exec(ctx, sqlf.Sprintf(deleteStaleExportedUploadsQuery, graphKey, pq.Array(ids))); err != nil {
		return 0, err
	}

	return len(ids), nil
}

// Note: should remove the cascsade delete on codeintel_ranking_exports
// from lsif_uploads, as we'd catch it this way without abandoning data
// in the bucket with no metadata to delete it from.

const selectStaleExportedUploadsQuery = `
SELECT re.upload_id
FROM codeintel_ranking_exports re
LEFT JOIN lsif_uploads u ON u.id = re.upload_id
LEFT JOIN repo r ON r.id = u.repository_id
WHERE
	re.graph_key = %s AND NOT (
		u.id IN (
			SELECT uvt.upload_id
			FROM lsif_uploads_visible_at_tip uvt
			WHERE uvt.is_default_branch
		) AND
		r.id IS NOT NULL AND
		r.deleted_at IS NULL AND
		r.blocked IS NULL
	)
ORDER BY re.upload_id DESC
LIMIT %s
FOR UPDATE OF re SKIP LOCKED
`

const deleteStaleExportedUploadsQuery = `
DELETE FROM codeintel_ranking_exports re
WHERE
	re.graph_key = %s AND
	re.upload_id = ANY(%q)
`
