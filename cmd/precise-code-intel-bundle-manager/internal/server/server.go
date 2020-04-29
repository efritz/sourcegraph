package server

import (
	"net"
	"net/http"
	"strconv"

	"github.com/inconshreveable/log15"
	"github.com/sourcegraph/sourcegraph/cmd/precise-code-intel-bundle-manager/internal/database"
	"github.com/sourcegraph/sourcegraph/internal/trace/ot"
)

type Server struct {
	host                 string
	port                 int
	bundleDir            string
	databaseCache        *database.DatabaseCache
	documentDataCache    *database.DocumentDataCache
	resultChunkDataCache *database.ResultChunkDataCache
}

type ServerOpts struct {
	Host                 string
	Port                 int
	BundleDir            string
	DatabaseCache        *database.DatabaseCache
	DocumentDataCache    *database.DocumentDataCache
	ResultChunkDataCache *database.ResultChunkDataCache
}

func New(opts ServerOpts) *Server {
	return &Server{
		host:                 opts.Host,
		port:                 opts.Port,
		bundleDir:            opts.BundleDir,
		databaseCache:        opts.DatabaseCache,
		documentDataCache:    opts.DocumentDataCache,
		resultChunkDataCache: opts.ResultChunkDataCache,
	}
}

func (s *Server) Start() error {
	addr := net.JoinHostPort(s.host, strconv.FormatInt(int64(s.port), 10))
	handler := ot.Middleware(s.handler())
	server := &http.Server{Addr: addr, Handler: handler}

	log15.Info("precise-code-intel-bundle-manager: listening", "addr", addr)
	return server.ListenAndServe()
}
