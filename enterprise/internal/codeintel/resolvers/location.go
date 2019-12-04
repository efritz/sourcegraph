package resolvers

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"

	"github.com/sourcegraph/sourcegraph/cmd/frontend/backend"
	"github.com/sourcegraph/sourcegraph/cmd/frontend/graphqlbackend"
	"github.com/sourcegraph/sourcegraph/cmd/frontend/graphqlbackend/graphqlutil"
	"github.com/sourcegraph/sourcegraph/enterprise/internal/codeintel/lsifserver/client"
	"github.com/sourcegraph/sourcegraph/internal/api"
	"github.com/sourcegraph/sourcegraph/internal/lsif"
)

//
// Connection Resolver

type LocationsQueryOptions struct {
	Operation  string
	Repository string
	Commit     string
	Path       string
	Line       int32
	Character  int32
	Limit      *int32
	NextURL    *string
}

type locationConnectionResolver struct {
	locations []*lsif.LSIFLocation
	nextURL   string
}

var _ graphqlbackend.LocationConnectionResolver = &locationConnectionResolver{}

func resolveLocationConnection(ctx context.Context, opt LocationsQueryOptions) (*locationConnectionResolver, error) {
	var path string
	if opt.NextURL == nil {
		// first page of results
		path = fmt.Sprintf("/%s", opt.Operation)
	} else {
		// subsequent page of results
		path = *opt.NextURL
	}

	values := url.Values{}
	values.Set("repository", opt.Repository)
	values.Set("commit", opt.Commit)
	values.Set("path", opt.Path)
	values.Set("line", strconv.FormatInt(int64(opt.Line), 10))
	values.Set("character", strconv.FormatInt(int64(opt.Character), 10))
	if opt.Limit != nil {
		values.Set("limit", strconv.FormatInt(int64(*opt.Limit), 10))
	}

	resp, err := client.DefaultClient.BuildAndTraceRequest(ctx, "GET", path, values, nil)
	if err != nil {
		return nil, err
	}

	payload := struct {
		Locations []*lsif.LSIFLocation
	}{
		Locations: []*lsif.LSIFLocation{},
	}

	if err := client.UnmarshalPayload(resp, &payload); err != nil {
		return nil, err
	}

	return &locationConnectionResolver{
		locations: payload.Locations,
		nextURL:   client.ExtractNextURL(resp),
	}, nil
}

func (r *locationConnectionResolver) Nodes(ctx context.Context) ([]graphqlbackend.LocationResolver, error) {
	var l []graphqlbackend.LocationResolver
	for _, location := range r.locations {
		resolver, err := rangeToLocationResolver(ctx, location)
		if err != nil {
			return nil, err
		}

		l = append(l, resolver)
	}

	return l, nil
}

func (r *locationConnectionResolver) PageInfo(ctx context.Context) (*graphqlutil.PageInfo, error) {
	if r.nextURL != "" {
		return graphqlutil.NextPageCursor(base64.StdEncoding.EncodeToString([]byte(r.nextURL))), nil
	}

	return graphqlutil.HasNextPage(false), nil
}

func (r *locationConnectionResolver) ToLocationConnection() (graphqlbackend.LocationConnectionResolver, bool) {
	return r, true
}

func (r *locationConnectionResolver) ToNoLSIFData() (graphqlbackend.NoLSIFDataResolver, bool) {
	return nil, false
}

//
// Helpers

func rangeToLocationResolver(ctx context.Context, location *lsif.LSIFLocation) (graphqlbackend.LocationResolver, error) {
	repo, err := backend.Repos.GetByName(ctx, api.RepoName(location.Repository))
	if err != nil {
		return nil, err
	}

	repoResolver, err := graphqlbackend.RepositoryByIDInt32(ctx, repo.ID)
	if err != nil {
		return nil, err
	}

	commitResolver, err := repoResolver.Commit(ctx, &graphqlbackend.RepositoryCommitArgs{Rev: location.Commit})
	if err != nil {
		return nil, err
	}

	gitTreeResolver, err := graphqlbackend.NewGitTreeEntryResolver(commitResolver, graphqlbackend.CreateFileInfo(location.Path, true)), nil
	if err != nil {
		return nil, err
	}

	return graphqlbackend.NewLocationResolver(gitTreeResolver, location.Range), nil
}
