package store

import (
	"context"
	"time"

	"github.com/sourcegraph/sourcegraph/internal/types"
)

type Store interface {
	// List returns a set of executor activity records matching the given options.
	//
	// 🚨 SECURITY: The caller must ensure that the actor is permitted to view executor details
	// (e.g., a site-admin).
	List(ctx context.Context, args ExecutorStoreListOptions) ([]types.Executor, int, error)

	// GetByID returns an executor activity record by identifier. If no such record exists, a
	// false-valued flag is returned.
	//
	// 🚨 SECURITY: The caller must ensure that the actor is permitted to view executor details
	// (e.g., a site-admin).
	GetByID(ctx context.Context, id int) (types.Executor, bool, error)

	// UpsertHeartbeat updates or creates an executor activity record for a particular executor instance.
	UpsertHeartbeat(ctx context.Context, executor types.Executor) error

	// DeleteInactiveHeartbeats deletes heartbeat records belonging to executor instances that have not pinged
	// the Sourcegraph instance in at least the given duration.
	DeleteInactiveHeartbeats(ctx context.Context, minAge time.Duration) error
}

type ExecutorStoreListOptions struct {
	Query  string
	Active bool
	Offset int
	Limit  int
}
