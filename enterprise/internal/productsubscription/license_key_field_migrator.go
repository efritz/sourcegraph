package productsubscription

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/keegancsmith/sqlf"
	"github.com/lib/pq"

	"github.com/sourcegraph/sourcegraph/enterprise/internal/license"
	"github.com/sourcegraph/sourcegraph/internal/database/basestore"
	"github.com/sourcegraph/sourcegraph/internal/database/dbutil"
	"github.com/sourcegraph/sourcegraph/lib/errors"
)

type licenseKeyFieldsMigrator struct {
	store *basestore.Store
}

func (m *licenseKeyFieldsMigrator) Progress(ctx context.Context) (float64, error) {
	progress, _, err := basestore.ScanFirstFloat(m.store.Query(ctx, sqlf.Sprintf(licenseKeyFieldsMigratorProgressQuery)))
	return progress, err
}

const licenseKeyFieldsMigratorProgressQuery = `
-- source: enterprise/internal/productsubscription/license_key_field_migrator.go:Progress
SELECT
	CASE c2.count WHEN 0 THEN 1 ELSE
		cast(c1.count as float) / cast(c2.count as float)
	END
FROM
	(SELECT count(*) as count FROM product_licenses WHERE license_tags IS NOT NULL) c1,
	(SELECT count(*) as count FROM product_licenses) c2
`

func (m *licenseKeyFieldsMigrator) Up(ctx context.Context) (err error) {
	tx, err := m.store.Transact(ctx)
	if err != nil {
		return errors.Wrap(err, "start transaction")
	}
	defer func() { err = tx.Done(err) }()

	updates, err := func() (updates []*sqlf.Query, err error) {
		// Select and lock a single record within this transaction. This ensures
		// that many worker instances can run the same migration concurrently
		// without them all trying to convert the same record.
		rows, err := tx.Query(ctx, sqlf.Sprintf(licenseKeyFieldsMigratorSelectQuery, 500))
		if err != nil {
			return nil, errors.Wrap(err, "query rows")
		}
		defer func() { err = basestore.CloseRows(rows, err) }()

		for rows.Next() {
			var id string
			var licenseKey string
			if err = rows.Scan(&id, &licenseKey); err != nil {
				return nil, errors.Wrap(err, "scan")
			}

			decodedText, err := base64.RawURLEncoding.DecodeString(licenseKey)
			if err != nil {
				return nil, errors.Wrap(err, "decode license key")
			}

			var decodedKey struct {
				Info []byte `json:"info"`
			}
			if err = json.Unmarshal(decodedText, &decodedKey); err != nil {
				return nil, errors.Wrap(err, "unmarshal decoded text")
			}

			var info license.Info
			if err = json.Unmarshal(decodedKey.Info, &info); err != nil {
				return nil, errors.Wrap(err, "unmarshal info")
			}

			var expiresAt *time.Time
			if !info.ExpiresAt.IsZero() {
				expiresAt = &info.ExpiresAt
			}
			updates = append(updates,
				sqlf.Sprintf("(%s, %s::integer, %s::text[], %s::integer, %s::timestamptz)",
					id,
					dbutil.NewNullInt64(int64(1)), // license_version
					pq.Array(info.Tags),           // license_tags
					dbutil.NewNullInt64(int64(info.UserCount)), // license_user_count
					dbutil.NullTime{Time: expiresAt}),          // license_expires_at
			)
		}

		return updates, nil
	}()
	if err != nil {
		return err
	}

	if err = tx.Exec(ctx, sqlf.Sprintf(licenseKeyFieldsMigratorUpdateQuery,
		sqlf.Join(updates, ", "),
	)); err != nil {
		return errors.Wrap(err, "update")
	}

	return nil
}

const licenseKeyFieldsMigratorSelectQuery = `
-- source: enterprise/internal/productsubscription/license_key_field_migrator.go:Up
SELECT
	id,
	license_key
FROM product_licenses
WHERE license_tags IS NULL
LIMIT %s
FOR UPDATE SKIP LOCKED
`

const licenseKeyFieldsMigratorUpdateQuery = `
-- source: enterprise/internal/productsubscription/license_key_field_migrator.go:Up
UPDATE product_licenses
SET
	license_version    = updates.license_version::integer,
	license_tags       = updates.license_tags::text[],
	license_user_count = updates.license_user_count::integer,
	license_expires_at = updates.license_expires_at::timestamptz
FROM (VALUES %s) AS updates(id, license_version, license_tags, license_user_count, license_expires_at)
WHERE product_licenses.id = updates.id::uuid
`

func (m *licenseKeyFieldsMigrator) Down(_ context.Context) error {
	// non-destructive
	return nil
}
