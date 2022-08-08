package migrations

import (
	"context"
	"strings"

	"github.com/keegancsmith/sqlf"

	"github.com/sourcegraph/log"

	"github.com/sourcegraph/sourcegraph/internal/database"
	"github.com/sourcegraph/sourcegraph/internal/database/basestore"
	"github.com/sourcegraph/sourcegraph/internal/extsvc"
	"github.com/sourcegraph/sourcegraph/internal/jsonc"
	"github.com/sourcegraph/sourcegraph/internal/oobmigration"
	"github.com/sourcegraph/sourcegraph/lib/errors"
	"github.com/sourcegraph/sourcegraph/schema"
)

type ExternalServiceWebhookMigrator struct {
	logger    log.Logger
	store     *basestore.Store
	BatchSize int
}

var _ oobmigration.Migrator = &ExternalServiceWebhookMigrator{}

func NewExternalServiceWebhookMigratorWithDB(db database.DB) *ExternalServiceWebhookMigrator {
	return &ExternalServiceWebhookMigrator{
		logger:    log.Scoped("ExternalServiceWebhookMigrator", ""),
		store:     basestore.NewWithHandle(db.Handle()),
		BatchSize: 50,
	}
}

func (m *ExternalServiceWebhookMigrator) ID() int {
	return 13
}

// Progress returns the percentage (ranged [0, 1]) of external services with a
// populated has_webhooks column.
func (m *ExternalServiceWebhookMigrator) Progress(ctx context.Context) (float64, error) {
	progress, _, err := basestore.ScanFirstFloat(m.store.Query(ctx, sqlf.Sprintf(externalServiceWebhookMigratorProgressQuery)))
	return progress, err
}

const externalServiceWebhookMigratorProgressQuery = `
-- source: internal/oobmigration/migrations/extsvc_webhook_migrator.go:Progress
SELECT
	CASE c2.count WHEN 0 THEN 1 ELSE
		CAST(c1.count AS float) / CAST(c2.count AS float)
	END
FROM
	(SELECT COUNT(*) AS count FROM external_services WHERE deleted_at IS NULL AND has_webhooks IS NOT NULL) c1,
	(SELECT COUNT(*) AS count FROM external_services WHERE deleted_at IS NULL) c2
`

// Up loads a set of external services without a populated has_webhooks column and
// updates that value by looking at that external service's configuration values.
func (m *ExternalServiceWebhookMigrator) Up(ctx context.Context) (err error) {
	var parseErrs error

	tx, err := m.store.Transact(ctx)
	if err != nil {
		return err
	}
	defer func() {
		// Commit transaction with non-parse errors. If we include parse errors in
		// this set prior to the tx.Done call, then we will always rollback the tx
		// and lose progress on the batch
		err = tx.Done(err)

		// Add non-"fatal" errors for callers
		err = errors.CombineErrors(err, parseErrs)
	}()

	type svc struct {
		ID           int
		Kind, Config string
	}
	svcs, err := func() (svcs []svc, err error) {
		rows, err := tx.Query(ctx, sqlf.Sprintf(externalServiceWebhookMigratorSelectQuery, m.BatchSize))
		if err != nil {
			return nil, err
		}
		defer func() { err = basestore.CloseRows(rows, err) }()

		for rows.Next() {
			var id int
			var kind, config, keyID string
			if err := rows.Scan(&id, &kind, &config, &keyID); err != nil {
				return nil, err
			}
			if keyID != "" {
				panic("UNSUPPORTED") // TODO
			}

			svcs = append(svcs, svc{ID: id, Kind: kind, Config: config})
		}

		return svcs, nil
	}()
	if err != nil {
		return err
	}

	for _, svc := range svcs {
		parseWebhooks := func(kind, config string) (bool, error) {
			switch strings.ToUpper(kind) {
			case extsvc.KindBitbucketServer:
				cfg := &schema.BitbucketServerConnection{}
				if err := jsonc.Unmarshal(config, cfg); err != nil {
					return false, err
				}

				return cfg.WebhookSecret() != "", nil

			case extsvc.KindGitHub:
				cfg := &schema.GitHubConnection{}
				if err := jsonc.Unmarshal(config, cfg); err != nil {
					return false, err
				}

				return len(cfg.Webhooks) > 0, nil

			case extsvc.KindGitLab:
				cfg := &schema.GitLabConnection{}
				if err := jsonc.Unmarshal(config, cfg); err != nil {
					return false, err
				}

				return len(cfg.Webhooks) > 0, nil
			}

			return false, nil
		}
		hasWebhooks, err := parseWebhooks(svc.Kind, svc.Config)
		if err != nil {
			parseErrs = errors.CombineErrors(parseErrs, err)
			continue
		}

		if err := tx.Exec(ctx, sqlf.Sprintf(externalServiceWebhookMigratorUpdateQuery, hasWebhooks, svc.ID)); err != nil {
			return err
		}
	}

	return nil
}

const externalServiceWebhookMigratorSelectQuery = `
-- source: internal/oobmigration/migrations/extsvc_webhook_migrator.go:Up
SELECT id, kind, config, encryption_key_id FROM external_services WHERE deleted_at IS NULL AND has_webhooks IS NULL ORDER BY id LIMIT %s FOR UPDATE
`

const externalServiceWebhookMigratorUpdateQuery = `
-- source: internal/oobmigration/migrations/extsvc_webhook_migrator.go:Up
UPDATE external_services SET has_webhooks = %s WHERE id = %s
`

func (*ExternalServiceWebhookMigrator) Down(context.Context) error {
	// non-destructive
	return nil
}
