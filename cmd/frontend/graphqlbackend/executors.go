package graphqlbackend

import (
	"context"

	"github.com/graph-gophers/graphql-go"

	"github.com/sourcegraph/sourcegraph/cmd/frontend/backend"
	gql "github.com/sourcegraph/sourcegraph/cmd/frontend/services/executors/graphql"
	"github.com/sourcegraph/sourcegraph/internal/database"
)

func (r *schemaResolver) Executors(ctx context.Context, args *struct {
	Query  *string
	Active *bool
	First  *int32
	After  *string
}) (*gql.ExecutorPaginatedResolver, error) {
	// 🚨 SECURITY: Only site-admins may view executor details
	if err := backend.CheckCurrentUserIsSiteAdmin(ctx, r.db); err != nil {
		return nil, err
	}

	executors, err := r.CodeIntelResolver.ExecutorResolver().Executors(ctx, args.Query, args.Active, args.First, args.After)
	if err != nil {
		return nil, err
	}

	return executors, nil
}

func executorByID(ctx context.Context, db database.DB, gqlID graphql.ID, r *schemaResolver) (*gql.ExecutorResolver, error) {
	if err := backend.CheckCurrentUserIsSiteAdmin(ctx, db); err != nil {
		return nil, err
	}

	executor, err := r.CodeIntelResolver.ExecutorResolver().Executor(ctx, gqlID)
	if err != nil {
		return nil, err
	}

	return executor, nil
}
