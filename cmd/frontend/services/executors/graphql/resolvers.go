package graphql

import (
	"context"

	"github.com/graph-gophers/graphql-go"
	"github.com/graph-gophers/graphql-go/relay"

	"github.com/sourcegraph/sourcegraph/cmd/frontend/graphqlbackend/graphqlutil"
	"github.com/sourcegraph/sourcegraph/cmd/frontend/services/executors"
	"github.com/sourcegraph/sourcegraph/internal/database/dbutil"
)

type Resolver interface {
	Executor(ctx context.Context, gqlID graphql.ID) (*ExecutorResolver, error)
	Executors(ctx context.Context, query *string, active *bool, first *int32, after *string) (*ExecutorPaginatedResolver, error)
}

type resolver struct {
	svc executors.Executor
}

func New(db dbutil.DB) Resolver {
	return &resolver{
		svc: executors.New(db),
	}
}

func (r *resolver) Executor(ctx context.Context, gqlID graphql.ID) (*ExecutorResolver, error) {
	id, err := unmarshalExecutorID(gqlID)
	if err != nil {
		return nil, err
	}

	executor, ok, err := r.svc.GetByID(ctx, int(id))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}

	return NewExecutorResolver(executor), nil
}

func (r *resolver) Executors(ctx context.Context, query *string, active *bool, first *int32, after *string) (*ExecutorPaginatedResolver, error) {
	p, err := validateArgs(ctx, query, active, first, after)
	if err != nil {
		return nil, err
	}

	executors, totalCount, err := r.svc.List(ctx, p.query, p.active, p.offset, p.limit)
	if err != nil {
		return nil, err
	}

	resolvers := make([]*ExecutorResolver, 0, len(executors))
	for _, executor := range executors {
		resolvers = append(resolvers, NewExecutorResolver(executor))
	}

	nextOffset := graphqlutil.NextOffset(p.offset, len(executors), totalCount)
	executorConnection := NewExecutorPaginatedConnection(resolvers, totalCount, nextOffset)

	return executorConnection, nil
}

func unmarshalExecutorID(id graphql.ID) (executorID int64, err error) {
	err = relay.UnmarshalSpec(id, &executorID)
	return
}
