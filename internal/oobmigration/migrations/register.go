package migrations

import (
	"context"
	"time"

	"github.com/sourcegraph/sourcegraph/internal/conf/conftypes"
	"github.com/sourcegraph/sourcegraph/internal/database"
	"github.com/sourcegraph/sourcegraph/internal/database/basestore"
	"github.com/sourcegraph/sourcegraph/internal/encryption/keyring"
	"github.com/sourcegraph/sourcegraph/internal/oobmigration"
	"github.com/sourcegraph/sourcegraph/internal/oobmigration/migrations/batches"
)

func RegisterOSSMigrations(ctx context.Context, db database.DB, runner *oobmigration.Runner) error {
	keyring := keyring.Default()

	return registerOSSMigrations(runner, false, migratorDependencies{
		store:   basestore.NewWithHandle(db.Handle()),
		keyring: &keyring,
	})
}

func RegisterOSSMigrationsFromConfig(ctx context.Context, db database.DB, runner *oobmigration.Runner, conf conftypes.UnifiedQuerier) error {
	keys, err := keyring.NewRing(ctx, conf.SiteConfig().EncryptionKeys)
	if err != nil {
		return err
	}
	if keys == nil {
		keys = &keyring.Ring{}
	}

	return registerOSSMigrations(runner, true, migratorDependencies{
		store:   basestore.NewWithHandle(db.Handle()),
		keyring: keys,
	})
}

type migratorDependencies struct {
	store   *basestore.Store
	keyring *keyring.Ring
}

func registerOSSMigrations(runner *oobmigration.Runner, noDelay bool, deps migratorDependencies) error {
	return RegisterAll(runner, noDelay, []TaggedMigrator{
		batches.NewExternalServiceWebhookMigratorWithDB(deps.store, deps.keyring.ExternalServiceKey, 50),
	})
}

type TaggedMigrator interface {
	oobmigration.Migrator
	ID() int
	Interval() time.Duration
}

func RegisterAll(runner *oobmigration.Runner, noDelay bool, migrators []TaggedMigrator) error {
	for _, migrator := range migrators {
		options := oobmigration.MigratorOptions{Interval: migrator.Interval()}
		if noDelay {
			options.Interval = time.Nanosecond
		}

		if err := runner.Register(migrator.ID(), migrator, options); err != nil {
			return err
		}
	}

	return nil
}
