package shared

import (
	"context"
	"database/sql"
	"os"

	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/urfave/cli/v2"

	"github.com/sourcegraph/log"

	connections "github.com/sourcegraph/sourcegraph/internal/database/connections/live"
	"github.com/sourcegraph/sourcegraph/internal/database/migration/cliutil"
	"github.com/sourcegraph/sourcegraph/internal/database/migration/schemas"
	"github.com/sourcegraph/sourcegraph/internal/database/migration/store"
	"github.com/sourcegraph/sourcegraph/internal/database/postgresdsn"
	"github.com/sourcegraph/sourcegraph/internal/observation"
	"github.com/sourcegraph/sourcegraph/internal/trace"
	"github.com/sourcegraph/sourcegraph/internal/version"
	"github.com/sourcegraph/sourcegraph/lib/output"
)

const appName = "migrator"

var out = output.NewOutput(os.Stdout, output.OutputOpts{
	ForceColor: true,
	ForceTTY:   true,
})

func Start() error {
	args := os.Args
	if len(args) == 1 {
		args = append(args, "up")
	}

	out.WriteLine(output.Linef(output.EmojiAsterisk, output.StyleReset, "Sourcegraph migrator v%s", version.Version()))

	ctx := context.Background()
	logger := log.Scoped("migrator.Start", "")
	outputFactory := func() *output.Output { return out }

	command := &cli.App{
		Name:   appName,
		Usage:  "Validates and runs schema migrations",
		Action: cli.ShowSubcommandHelp,
		Commands: []*cli.Command{
			cliutil.Up(appName, newRunner, outputFactory, false),
			cliutil.UpTo(appName, newRunner, outputFactory, false),
			cliutil.DownTo(appName, newRunner, outputFactory, false),
			cliutil.Validate(appName, newRunner, outputFactory),
			cliutil.Describe(appName, newRunner, outputFactory),
			cliutil.Drift(appName, newRunner, outputFactory, cliutil.GCSExpectedSchemaFactory, cliutil.GitHubExpectedSchemaFactory),
			cliutil.AddLog(logger, appName, newRunner, outputFactory),
			cliutil.Upgrade(logger, appName, newRunnerWithSchemas, outputFactory),
		},
	}

	return command.RunContext(ctx, args)
}

func newRunner(ctx context.Context, schemaNames []string) (cliutil.Runner, error) {
	return newRunnerWithSchemas(ctx, schemaNames, schemas.Schemas)
}

func newRunnerWithSchemas(ctx context.Context, schemaNames []string, schemas []*schemas.Schema) (cliutil.Runner, error) {
	logger := log.Scoped("runner", "")
	observationContext := &observation.Context{
		Logger:     logger,
		Tracer:     &trace.Tracer{Tracer: opentracing.GlobalTracer()},
		Registerer: prometheus.DefaultRegisterer,
	}
	operations := store.NewOperations(observationContext)

	dsns, err := postgresdsn.DSNsBySchema(schemaNames)
	if err != nil {
		return nil, err
	}
	storeFactory := func(db *sql.DB, migrationsTable string) connections.Store {
		return connections.NewStoreShim(store.NewWithDB(db, migrationsTable, operations))
	}
	r, err := connections.RunnerFromDSNsWithSchemas(logger, dsns, appName, storeFactory, schemas)
	if err != nil {
		return nil, err
	}

	return cliutil.NewShim(r), nil
}
